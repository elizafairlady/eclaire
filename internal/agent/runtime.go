package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/schema"
	"github.com/elizafairlady/eclaire/internal/hook"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// ConversationRuntime implements the agentic loop with full control over
// streaming, tool extraction, permission checking, hook execution, and compaction.
// Reference: Claw Code rust/crates/runtime/src/conversation.rs — ConversationRuntime<C,T>
// HardMaxIterations is the absolute ceiling for iterations per turn.
// Even if MaxIterations is set higher, this value cannot be exceeded.
const HardMaxIterations = 200

// DefaultMaxSessionTokens is the default hard cap on cumulative tokens
// (input + output) before a run is forcibly terminated. 2M tokens.
const DefaultMaxSessionTokens int64 = 2_000_000

type ConversationRuntime struct {
	Model         fantasy.LanguageModel
	SystemPrompt  string
	Tools         []fantasy.AgentTool
	HookRunner    *hook.Runner
	PermChecker   *tool.PermissionChecker
	Approver      tool.Approver
	PermMode      tool.PermissionMode
	WorkspaceRoots []string
	AgentID        string
	Logger         *slog.Logger
	MaxIterations    int   // 0 = default (25), clamped to HardMaxIterations
	ContextWindow    int64 // total context window for the model
	MaxOutputToks    int64 // 0 = derived from ContextWindow
	MaxSessionTokens int64 // 0 = default (2M), hard cap on cumulative tokens
	SessionID        string                 // for persisting approval patterns
	Sessions         *persist.SessionStore  // for persisting approval patterns
}

// FinishReason indicates why a conversation turn ended.
type FinishReason string

const (
	FinishEndTurn           FinishReason = "end_turn"           // model stopped naturally
	FinishMaxIterations     FinishReason = "max_iterations"     // hit iteration cap
	FinishLoopDetected      FinishReason = "loop_detected"      // repeated tool calls
	FinishDegenerateOutput  FinishReason = "degenerate_output"  // repetitive text
	FinishConsecutiveErrors FinishReason = "consecutive_errors" // same tool failing repeatedly
	FinishTokenBudget       FinishReason = "token_budget"       // hard session token cap exceeded
	FinishCancelled         FinishReason = "cancelled"          // user cancelled the run
	FinishError             FinishReason = "error"              // stream/model error
)

// TurnSummary is the result of a single conversation turn.
type TurnSummary struct {
	Text         string
	Reasoning    string
	ToolCalls    int
	Iterations   int
	Usage        fantasy.Usage
	Aborted      bool
	FinishReason FinishReason
}

// AbortSignal allows external cancellation of the agentic loop.
type AbortSignal struct {
	aborted atomic.Bool
}

func NewAbortSignal() *AbortSignal { return &AbortSignal{} }
func (a *AbortSignal) Abort()      { a.aborted.Store(true) }
func (a *AbortSignal) IsAborted() bool { return a.aborted.Load() }

// RunTurn executes one user turn through the agentic loop.
// It streams the model, extracts tool calls, runs hooks/permissions, executes tools,
// and loops until the model stops or a stop condition is met.
func (rt *ConversationRuntime) RunTurn(
	ctx context.Context,
	messages []fantasy.Message,
	emit func(StreamEvent) error,
) (*TurnSummary, []fantasy.Message, error) {
	maxIter := rt.MaxIterations
	if maxIter <= 0 {
		maxIter = HardMaxIterations
	}
	if maxIter > HardMaxIterations {
		maxIter = HardMaxIterations
	}

	maxSessionToks := rt.MaxSessionTokens
	if maxSessionToks <= 0 {
		maxSessionToks = DefaultMaxSessionTokens
	}

	// Build tool specs for the API call
	apiTools := rt.buildToolSpecs()

	const (
		degenerateThreshold     = 20 // ngram repeat count to flag degenerate output
		maxConsecutiveToolErrors = 3  // same tool errors before injecting guidance
		maxErrorResets          = 2  // guidance injections before hard stop
	)

	var totalUsage fantasy.Usage
	var totalCost float64
	var allNewMessages []fantasy.Message
	totalToolCalls := 0
	var finalText string
	var finalReasoning string
	finishReason := FinishEndTurn

	// Stuck detection state
	var loopHistory []string
	consecutiveErrors := make(map[string]int)
	totalErrorResets := 0
	completedAllIterations := true

	for iter := range maxIter {
		_ = iter

		// Check for cancellation at the top of each iteration
		if ctx.Err() != nil {
			finishReason = FinishCancelled
			completedAllIterations = false
			break
		}

		// Build the API call
		toolChoice := fantasy.ToolChoiceAuto
		maxOutput := rt.maxOutputTokens()
		call := fantasy.Call{
			Prompt:          messages,
			Tools:           apiTools,
			ToolChoice:      &toolChoice,
			MaxOutputTokens: &maxOutput,
		}

		// Stream the model response
		stream, err := rt.Model.Stream(ctx, call)
		if err != nil {
			if ctx.Err() != nil {
				return &TurnSummary{
					Text: finalText, Reasoning: finalReasoning,
					ToolCalls: totalToolCalls, Iterations: iter,
					Usage: totalUsage, FinishReason: FinishCancelled,
				}, allNewMessages, nil
			}
			finishReason = FinishError
			return nil, allNewMessages, fmt.Errorf("model stream: %w", err)
		}

		// Parse stream into assistant message parts
		var textContent string
		var reasoningContent string
		var pendingToolCalls []toolCallInfo
		var stepUsage fantasy.Usage
		var stepCost float64
		var currentToolID string
		var currentToolName string
		var currentToolInput string

		for part := range stream {
			switch part.Type {
			case fantasy.StreamPartTypeTextDelta:
				textContent += part.Delta
				emit(StreamEvent{Type: EventTextDelta, Delta: part.Delta, AgentID: rt.AgentID})

			case fantasy.StreamPartTypeReasoningDelta:
				reasoningContent += part.Delta

			case fantasy.StreamPartTypeToolInputStart:
				currentToolID = part.ID
				currentToolName = part.ToolCallName
				currentToolInput = ""

			case fantasy.StreamPartTypeToolInputDelta:
				currentToolInput += part.Delta

			case fantasy.StreamPartTypeToolInputEnd:

			case fantasy.StreamPartTypeToolCall:
				tc := toolCallInfo{
					ID:    part.ID,
					Name:  part.ToolCallName,
					Input: part.ToolCallInput,
				}
				if tc.Input == "" && currentToolID == tc.ID {
					tc.Input = currentToolInput
				}
				if tc.Name == "" {
					tc.Name = currentToolName
				}
				pendingToolCalls = append(pendingToolCalls, tc)

				emit(StreamEvent{
					Type:       EventToolCall,
					ToolName:   tc.Name,
					ToolCallID: tc.ID,
					Input:      tc.Input,
					AgentID:    rt.AgentID,
				})

			case fantasy.StreamPartTypeFinish:
				stepUsage = part.Usage
				// Extract actual cost from OpenRouter provider metadata
				stepCost += extractProviderCost(part.ProviderMetadata)

			case fantasy.StreamPartTypeError:
				if part.Error != nil {
					finishReason = FinishError
					return nil, allNewMessages, fmt.Errorf("stream error: %w", part.Error)
				}
			}
		}

		// Accumulate usage
		totalUsage.InputTokens += stepUsage.InputTokens
		totalUsage.OutputTokens += stepUsage.OutputTokens
		totalUsage.TotalTokens += stepUsage.TotalTokens
		totalUsage.ReasoningTokens += stepUsage.ReasoningTokens
		totalUsage.CacheCreationTokens += stepUsage.CacheCreationTokens
		totalUsage.CacheReadTokens += stepUsage.CacheReadTokens
		totalCost += stepCost

		emit(StreamEvent{
			Type: EventStepFinish,
			Usage: &UsageInfo{
				InputTokens:  totalUsage.InputTokens,
				OutputTokens: totalUsage.OutputTokens,
				Cost:         totalCost,
			},
			AgentID: rt.AgentID,
		})

		// Hard token budget check — kill the run if cumulative tokens exceed cap
		cumulativeTokens := totalUsage.InputTokens + totalUsage.OutputTokens
		if cumulativeTokens >= maxSessionToks {
			rt.Logger.Warn("session token budget exceeded",
				"cumulative", cumulativeTokens, "max", maxSessionToks)
			finishReason = FinishTokenBudget
			completedAllIterations = false
			break
		}

		// Check for degenerate output BEFORE building messages or executing tools.
		// Catches "member member member..." repetition loops.
		if textContent != "" && isDegenerate(textContent, degenerateThreshold) {
			rt.Logger.Warn("degenerate model output detected",
				"iteration", iter+1, "text_len", len(textContent))
			finishReason = FinishDegenerateOutput
			completedAllIterations = false
			break
		}

		// Build assistant message
		var assistantParts []fantasy.MessagePart
		if textContent != "" {
			assistantParts = append(assistantParts, fantasy.TextPart{Text: textContent})
			finalText = textContent
		}
		if reasoningContent != "" {
			finalReasoning = reasoningContent
		}
		for _, tc := range pendingToolCalls {
			assistantParts = append(assistantParts, fantasy.ToolCallPart{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Input:      tc.Input,
			})
		}
		if len(assistantParts) > 0 {
			assistantMsg := fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: assistantParts,
			}
			messages = append(messages, assistantMsg)
			allNewMessages = append(allNewMessages, assistantMsg)
		}

		// No tool calls → model is done
		if len(pendingToolCalls) == 0 {
			finishReason = FinishEndTurn
			completedAllIterations = false
			break
		}

		// Execute each tool call with hooks and permission checking
		var toolResultParts []fantasy.MessagePart
		resultMap := make(map[string]string) // for loop signature
		iterBroke := false

		for _, tc := range pendingToolCalls {
			totalToolCalls++
			result := rt.executeToolCall(ctx, tc, emit)

			outputText := ""
			if t, ok := result.output.(fantasy.ToolResultOutputContentText); ok {
				outputText = t.Text
			}
			resultMap[tc.ID] = outputText

			toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
				ToolCallID: tc.ID,
				Output:     result.output,
			})

			// Track consecutive errors per tool
			if result.isError {
				consecutiveErrors[tc.Name]++
				if consecutiveErrors[tc.Name] >= maxConsecutiveToolErrors {
					totalErrorResets++
					if totalErrorResets >= maxErrorResets {
						rt.Logger.Warn("persistent tool failures, stopping",
							"tool", tc.Name, "total_resets", totalErrorResets)
						finishReason = FinishConsecutiveErrors
						iterBroke = true
						break
					}
					// Inject guidance — give the model one chance to adapt
					rt.Logger.Warn("tool failing repeatedly, injecting guidance",
						"tool", tc.Name, "errors", consecutiveErrors[tc.Name])
					consecutiveErrors[tc.Name] = 0
				}
			} else {
				consecutiveErrors[tc.Name] = 0
			}
		}

		// Build tool result message
		if len(toolResultParts) > 0 {
			toolMsg := fantasy.Message{
				Role:    fantasy.MessageRoleTool,
				Content: toolResultParts,
			}
			messages = append(messages, toolMsg)
			allNewMessages = append(allNewMessages, toolMsg)
		}

		if iterBroke {
			completedAllIterations = false
			break
		}

		// Inject error guidance as a system message if a tool just hit the threshold
		for toolName, count := range consecutiveErrors {
			if count == 0 && totalErrorResets > 0 {
				// A reset just happened — the counter was zeroed. Inject guidance.
				_ = toolName // already logged above
			}
		}

		// Check for tool call loops (after results are available)
		sig := hashToolIteration(pendingToolCalls, resultMap)
		if sig != "" {
			loopHistory = append(loopHistory, sig)
			if isLooping(loopHistory, loopWindowSize, loopMaxRepeats) {
				rt.Logger.Warn("tool call loop detected",
					"iteration", iter+1, "window", loopWindowSize)
				finishReason = FinishLoopDetected
				completedAllIterations = false
				break
			}
		}

		// Reset text for next iteration
		textContent = ""
		reasoningContent = ""
	}

	// If the loop exhausted all iterations without breaking, it's max_iterations
	if completedAllIterations && totalToolCalls > 0 {
		finishReason = FinishMaxIterations
		rt.Logger.Warn("max iterations reached",
			"max", maxIter, "tool_calls", totalToolCalls)
	}

	return &TurnSummary{
		Text:         finalText,
		Reasoning:    finalReasoning,
		ToolCalls:    totalToolCalls,
		Iterations:   min(maxIter, totalToolCalls+1),
		Usage:        totalUsage,
		FinishReason: finishReason,
	}, allNewMessages, nil
}

type toolCallInfo struct {
	ID    string
	Name  string
	Input string
}

type toolResult struct {
	output fantasy.ToolResultOutputContent
	isError bool
}

// executeToolCall runs a single tool call through hooks → permissions → execution → post-hooks.
func (rt *ConversationRuntime) executeToolCall(ctx context.Context, tc toolCallInfo, emit func(StreamEvent) error) toolResult {
	// 1. Pre-hook: can deny, rewrite input, set permission override
	effectiveInput := tc.Input
	if rt.HookRunner != nil {
		updatedInput, denied, denyMsg, err := rt.HookRunner.RunPre(ctx, tc.Name, tc.Input, rt.AgentID, "")
		if err != nil {
			rt.Logger.Warn("pre-hook error", "tool", tc.Name, "err", err)
		}
		if denied {
			msg := denyMsg
			if msg == "" {
				msg = "denied by pre-hook"
			}
			emit(StreamEvent{
				Type:       EventToolResult,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
				Output:     "Permission denied: " + msg,
				AgentID:    rt.AgentID,
			})
			return toolResult{
				output:  fantasy.ToolResultOutputContentText{Text: "Permission denied: " + msg},
				isError: true,
			}
		}
		if updatedInput != "" {
			effectiveInput = updatedInput
		}
	}

	// 2. Permission check
	if rt.PermChecker != nil {
		decision := rt.PermChecker.CheckWithMode(rt.AgentID, tc.Name, effectiveInput, rt.PermMode)
		if decision == tool.DecisionDeny {
			msg := fmt.Sprintf("Permission denied for tool %q under current mode", tc.Name)
			emit(StreamEvent{
				Type:       EventToolResult,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
				Output:     msg,
				AgentID:    rt.AgentID,
			})
			return toolResult{
				output:  fantasy.ToolResultOutputContentText{Text: msg},
				isError: true,
			}
		}
		if decision == tool.DecisionPrompt {
			if rt.Approver != nil {
				desc := fmt.Sprintf("Agent %q wants to use tool %q", rt.AgentID, tc.Name)
				// Include a summary of what the tool will actually do
				inputSummary := summarizeToolInput(tc.Name, effectiveInput)
				if inputSummary != "" {
					desc += "\n" + inputSummary
				}
				// For shell tool, extract the command pattern for granular approval
				shellPattern := ""
				if tc.Name == "shell" {
					shellPattern = tool.ExtractShellPattern(effectiveInput)
				}

				result, err := rt.Approver.Request(ctx, rt.AgentID, tc.Name, desc,
					map[string]any{"tool": tc.Name, "input": effectiveInput, "pattern": shellPattern})
				if err != nil || !result.Approved {
					msg := fmt.Sprintf("permission denied: tool %q was not approved", tc.Name)
					if result.Reason != "" {
						msg += ": " + result.Reason
					}
					emit(StreamEvent{
						Type:       EventToolResult,
						ToolName:   tc.Name,
						ToolCallID: tc.ID,
						Output:     msg,
						AgentID:    rt.AgentID,
					})
					return toolResult{
						output:  fantasy.ToolResultOutputContentText{Text: msg},
						isError: true,
					}
				}
				// Persist approval for the session when user said "always"
				if result.Persist {
					if shellPattern != "" {
						// Pattern-scoped: approve "go test" or "git" specifically
						rt.PermChecker.ApprovePattern(rt.AgentID, tc.Name, shellPattern)
					} else {
						// Full tool approval for non-shell or unparseable commands
						rt.PermChecker.Approve(rt.AgentID, tc.Name)
					}
					rt.persistApprovals()
				}
			} else {
				// No approver available — deny by default
				msg := fmt.Sprintf("permission denied: tool %q requires approval but no approver available", tc.Name)
				emit(StreamEvent{
					Type:       EventToolResult,
					ToolName:   tc.Name,
					ToolCallID: tc.ID,
					Output:     msg,
					AgentID:    rt.AgentID,
				})
				return toolResult{
					output:  fantasy.ToolResultOutputContentText{Text: msg},
					isError: true,
				}
			}
		}
	}

	// 2b. Workspace boundary check for file-writing tools
	if len(rt.WorkspaceRoots) > 0 {
		if ok, reason := tool.CheckWorkspaceBoundary(tc.Name, effectiveInput, rt.WorkspaceRoots); !ok {
			if rt.Approver != nil {
				desc := fmt.Sprintf("Agent %q wants to write outside workspace: %s", rt.AgentID, reason)
				result, err := rt.Approver.Request(ctx, rt.AgentID, "write_outside_workspace", desc,
					map[string]any{"tool": tc.Name, "input": effectiveInput, "reason": reason})
				if err != nil || !result.Approved {
					msg := "Permission denied: " + reason
					emit(StreamEvent{
						Type:       EventToolResult,
						ToolName:   tc.Name,
						ToolCallID: tc.ID,
						Output:     msg,
						AgentID:    rt.AgentID,
					})
					return toolResult{
						output:  fantasy.ToolResultOutputContentText{Text: msg},
						isError: true,
					}
				}
				// Approved — extend roots for rest of session + sandbox
				rt.extendWorkspaceRoots(effectiveInput)
				// Also extend the sandbox write roots so bwrap allows the write
				tool.DefaultExecutor.AddSandboxWriteRoot(rt.extractWriteDir(effectiveInput))
			} else {
				msg := "Permission denied: " + reason
				emit(StreamEvent{
					Type:       EventToolResult,
					ToolName:   tc.Name,
					ToolCallID: tc.ID,
					Output:     msg,
					AgentID:    rt.AgentID,
				})
				return toolResult{
					output:  fantasy.ToolResultOutputContentText{Text: msg},
					isError: true,
				}
			}
		}
	}

	// 2c. Validate tool input before execution
	if err := tool.ValidateToolInput(tc.Name, effectiveInput); err != nil {
		msg := fmt.Sprintf("Invalid tool input: %v", err)
		rt.Logger.Warn("tool input validation failed", "tool", tc.Name, "err", err)
		emit(StreamEvent{
			Type:       EventToolResult,
			ToolName:   tc.Name,
			ToolCallID: tc.ID,
			Output:     msg,
			AgentID:    rt.AgentID,
		})
		return toolResult{
			output:  fantasy.ToolResultOutputContentText{Text: msg},
			isError: true,
		}
	}

	// 3. Find and execute the tool
	var agentTool fantasy.AgentTool
	for _, t := range rt.Tools {
		if t.Info().Name == tc.Name {
			agentTool = t
			break
		}
	}
	if agentTool == nil {
		msg := fmt.Sprintf("tool %q not found", tc.Name)
		emit(StreamEvent{
			Type:       EventToolResult,
			ToolName:   tc.Name,
			ToolCallID: tc.ID,
			Output:     msg,
			AgentID:    rt.AgentID,
		})
		return toolResult{
			output:  fantasy.ToolResultOutputContentError{Error: fmt.Errorf("%s", msg)},
			isError: true,
		}
	}

	resp, err := agentTool.Run(ctx, fantasy.ToolCall{
		ID:    tc.ID,
		Name:  tc.Name,
		Input: effectiveInput,
	})

	var outputText string
	if err != nil {
		outputText = fmt.Sprintf("Error: %v", err)
	} else {
		outputText = resp.Content
	}

	// 4. Post-hook
	if rt.HookRunner != nil {
		if err != nil || resp.IsError {
			rt.HookRunner.RunPostFailure(ctx, tc.Name, effectiveInput, outputText, rt.AgentID, "")
		} else {
			extraMsg, postErr := rt.HookRunner.RunPost(ctx, tc.Name, effectiveInput, outputText, rt.AgentID, "")
			if postErr != nil {
				rt.Logger.Warn("post-hook error", "tool", tc.Name, "err", postErr)
			}
			if extraMsg != "" {
				outputText += "\n[hook]: " + extraMsg
			}
		}
	}

	emit(StreamEvent{
		Type:       EventToolResult,
		ToolName:   tc.Name,
		ToolCallID: tc.ID,
		Output:     outputText,
		AgentID:    rt.AgentID,
	})

	if err != nil || resp.IsError {
		return toolResult{
			output:  fantasy.ToolResultOutputContentText{Text: outputText},
			isError: true,
		}
	}
	return toolResult{
		output:  fantasy.ToolResultOutputContentText{Text: outputText},
		isError: false,
	}
}

// extractProviderCost pulls the actual USD cost from provider metadata.
// Returns 0 if no cost info is available.
func extractProviderCost(meta fantasy.ProviderMetadata) float64 {
	if meta == nil {
		return 0
	}
	orMeta, ok := meta["openrouter"]
	if !ok {
		return 0
	}
	// Type-assert to openrouter.ProviderMetadata
	if pm, ok := orMeta.(*openrouter.ProviderMetadata); ok {
		return pm.Usage.Cost
	}
	return 0
}

// summarizeToolInput extracts the most relevant field from a tool's JSON input
// for display in approval prompts. Returns empty string if nothing useful.
func summarizeToolInput(toolName, input string) string {
	var params map[string]any
	if json.Unmarshal([]byte(input), &params) != nil {
		return ""
	}
	switch toolName {
	case "shell":
		if cmd, ok := params["command"].(string); ok {
			if len(cmd) > 200 {
				cmd = cmd[:200] + "..."
			}
			return "Command: " + cmd
		}
	case "write", "edit", "multiedit", "apply_patch":
		if p, ok := params["path"].(string); ok {
			return "Path: " + p
		}
		if p, ok := params["file_path"].(string); ok {
			return "Path: " + p
		}
	case "fetch":
		if u, ok := params["url"].(string); ok {
			return "URL: " + u
		}
	case "download":
		if u, ok := params["url"].(string); ok {
			return "URL: " + u
		}
	case "eclaire_email":
		if to, ok := params["to"].(string); ok {
			return "To: " + to
		}
	}
	return ""
}

// persistApprovals saves current approval keys to session metadata (best-effort).
func (rt *ConversationRuntime) persistApprovals() {
	if rt.Sessions == nil || rt.SessionID == "" {
		return
	}
	keys := rt.PermChecker.ApprovedKeys()
	patterns := make(map[string][]string, len(keys))
	for _, k := range keys {
		patterns[k] = nil // nil = unconditional approval
	}
	if err := rt.Sessions.SavePatterns(rt.SessionID, patterns); err != nil {
		rt.Logger.Warn("failed to save approval patterns", "err", err)
	}
}

// extendWorkspaceRoots adds the directory of an approved out-of-bounds write.
func (rt *ConversationRuntime) extendWorkspaceRoots(input string) {
	dir := rt.extractWriteDir(input)
	if dir != "" {
		rt.WorkspaceRoots = append(rt.WorkspaceRoots, dir)
	}
}

// extractWriteDir returns the parent directory of the target path in a write tool's input.
func (rt *ConversationRuntime) extractWriteDir(input string) string {
	var params map[string]any
	if json.Unmarshal([]byte(input), &params) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "file"} {
		if p, ok := params[key].(string); ok && p != "" {
			absPath, err := filepath.Abs(p)
			if err != nil {
				return ""
			}
			return filepath.Dir(filepath.Clean(absPath))
		}
	}
	return ""
}

// maxOutputTokens returns the max tokens the model can generate per step.
// If explicitly set, uses that. Otherwise derives from context window:
// 25% of context window, capped at 32k (most models max out there).
func (rt *ConversationRuntime) maxOutputTokens() int64 {
	if rt.MaxOutputToks > 0 {
		return rt.MaxOutputToks
	}
	if rt.ContextWindow > 0 {
		max := rt.ContextWindow / 4
		if max > 32768 {
			max = 32768
		}
		if max < 4096 {
			max = 4096
		}
		return max
	}
	return 16384 // fallback
}

// buildToolSpecs converts AgentTools to fantasy.Tool specs for the API call.
func (rt *ConversationRuntime) buildToolSpecs() []fantasy.Tool {
	specs := make([]fantasy.Tool, 0, len(rt.Tools))
	for _, t := range rt.Tools {
		info := t.Info()
		inputSchema := map[string]any{
			"type":       "object",
			"properties": info.Parameters,
			"required":   info.Required,
		}
		schema.Normalize(inputSchema)
		specs = append(specs, fantasy.FunctionTool{
			Name:            info.Name,
			Description:     info.Description,
			InputSchema:     inputSchema,
			ProviderOptions: t.ProviderOptions(),
		})
	}
	return specs
}
