package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/tool"
	"gopkg.in/yaml.v3"
)

// FlowStep is a single step in a flow pipeline.
type FlowStep struct {
	Name   string `yaml:"name" json:"name"`
	Agent  string `yaml:"agent" json:"agent"`
	Prompt string `yaml:"prompt" json:"prompt"` // Go template: {{.Input}}, {{.PrevOutput}}
}

// FlowDef defines a multi-step pipeline.
type FlowDef struct {
	ID          string     `yaml:"id" json:"id"`
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description,omitempty" json:"description,omitempty"`
	Steps       []FlowStep `yaml:"steps" json:"steps"`
}

// FlowStatus tracks a flow run's lifecycle.
type FlowStatus string

const (
	FlowRunning   FlowStatus = "running"
	FlowCompleted FlowStatus = "completed"
	FlowFailed    FlowStatus = "failed"
)

// FlowRun is the state of an executing flow.
type FlowRun struct {
	ID          string     `json:"id"`
	FlowDef     FlowDef    `json:"flow_def"`
	Status      FlowStatus `json:"status"`
	CurrentStep int        `json:"current_step"`
	StepOutputs []string   `json:"step_outputs"`
	Input       string     `json:"input"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// FlowExecutor runs multi-step flow pipelines.
type FlowExecutor struct {
	Runner   *Runner
	Tasks    *TaskRegistry
	Registry *Registry
	Bus      *bus.Bus
	Logger   *slog.Logger
}

// Run executes a flow definition with the given input.
func (e *FlowExecutor) Run(ctx context.Context, def FlowDef, input string, emit func(StreamEvent) error) (*FlowRun, error) {
	flowID := fmt.Sprintf("flow_%s_%d", def.ID, time.Now().UnixNano())

	run := &FlowRun{
		ID:        flowID,
		FlowDef:   def,
		Status:    FlowRunning,
		Input:     input,
		CreatedAt: time.Now(),
	}

	e.Logger.Info("flow started", "flow", def.ID, "steps", len(def.Steps))
	e.Bus.Publish(bus.TopicFlowStarted, bus.FlowEvent{
		FlowID: flowID,
		Name:   def.Name,
		Steps:  len(def.Steps),
	})
	emit(StreamEvent{
		Type:   "flow_started",
		Output: fmt.Sprintf("Starting flow %q with %d steps", def.Name, len(def.Steps)),
	})

	prevOutput := ""
	for i, step := range def.Steps {
		run.CurrentStep = i

		// Resolve agent
		a, ok := e.Registry.Get(step.Agent)
		if !ok {
			run.Status = FlowFailed
			run.Error = fmt.Sprintf("step %d (%s): agent %q not found", i, step.Name, step.Agent)
			return run, fmt.Errorf("%s", run.Error)
		}

		// Template the prompt
		prompt, err := templatePrompt(step.Prompt, input, prevOutput)
		if err != nil {
			run.Status = FlowFailed
			run.Error = fmt.Sprintf("step %d (%s): template error: %v", i, step.Name, err)
			return run, fmt.Errorf("%s", run.Error)
		}

		// Create task
		taskID := fmt.Sprintf("%s_step_%d", flowID, i)
		e.Tasks.Create(taskID, step.Agent, prompt)
		e.Tasks.SetFlowID(taskID, flowID)
		e.Tasks.UpdateStatus(taskID, TaskRunning, "", "")

		e.Logger.Info("flow step started", "flow", flowID, "step", i, "name", step.Name, "agent", step.Agent)
		emit(StreamEvent{
			Type:    "flow_step_started",
			AgentID: step.Agent,
			Output:  fmt.Sprintf("Step %d/%d: %s (agent: %s)", i+1, len(def.Steps), step.Name, step.Agent),
		})

		// Run the agent
		result, runErr := e.Runner.Run(ctx, RunConfig{
			AgentID:        step.Agent,
			Agent:          a,
			Prompt:         prompt,
			PromptMode:     PromptModeMinimal,
			PermissionMode: tool.PermissionWriteOnly,
		}, func(ev StreamEvent) error {
			// Forward sub-events with flow context
			ev.TaskID = taskID
			return emit(ev)
		})

		if runErr != nil {
			e.Tasks.UpdateStatus(taskID, TaskFailed, "", runErr.Error())
			run.Status = FlowFailed
			run.Error = fmt.Sprintf("step %d (%s): %v", i, step.Name, runErr)
			e.Bus.Publish(bus.TopicFlowCompleted, bus.FlowEvent{
				FlowID: flowID, Name: def.Name, Status: "failed", Error: run.Error,
			})
			return run, fmt.Errorf("%s", run.Error)
		}

		output := result.Content
		e.Tasks.UpdateStatus(taskID, TaskCompleted, output, "")
		if result.SessionID != "" {
			e.Tasks.SetSessionID(taskID, result.SessionID)
		}

		run.StepOutputs = append(run.StepOutputs, output)
		prevOutput = output

		emit(StreamEvent{
			Type:    "flow_step_completed",
			AgentID: step.Agent,
			Output:  fmt.Sprintf("Step %d/%d complete: %s", i+1, len(def.Steps), step.Name),
		})
	}

	run.Status = FlowCompleted
	e.Logger.Info("flow completed", "flow", flowID, "steps", len(def.Steps))
	e.Bus.Publish(bus.TopicFlowCompleted, bus.FlowEvent{
		FlowID: flowID, Name: def.Name, Status: "completed",
	})
	emit(StreamEvent{
		Type:   "flow_completed",
		Output: fmt.Sprintf("Flow %q completed (%d steps)", def.Name, len(def.Steps)),
	})

	return run, nil
}

// templatePrompt applies Go template substitution to a step prompt.
func templatePrompt(tmplStr, input, prevOutput string) (string, error) {
	// Simple string replacement for common cases
	result := strings.ReplaceAll(tmplStr, "{{.Input}}", input)
	result = strings.ReplaceAll(result, "{{.PrevOutput}}", prevOutput)

	// If no template markers remain, return as-is
	if !strings.Contains(result, "{{") {
		return result, nil
	}

	// Fall back to full Go template for complex cases
	tmpl, err := template.New("step").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	data := struct {
		Input      string
		PrevOutput string
	}{input, prevOutput}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// LoadFlowFile loads a flow definition from a YAML file.
func LoadFlowFile(path string) (*FlowDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def FlowDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if def.ID == "" {
		base := filepath.Base(path)
		def.ID = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if def.Name == "" {
		def.Name = def.ID
	}
	if len(def.Steps) == 0 {
		return nil, fmt.Errorf("flow %s has no steps", def.ID)
	}
	return &def, nil
}

// LoadFlowsDir loads all flow definitions from a directory.
func LoadFlowsDir(dir string) ([]*FlowDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var flows []*FlowDef
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		def, err := LoadFlowFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		flows = append(flows, def)
	}
	return flows, nil
}
