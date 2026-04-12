package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"charm.land/fantasy"
)

// MockModel implements fantasy.LanguageModel for testing.
// Returns scripted responses, optionally including tool calls.
type MockModel struct {
	Responses []MockResponse
	callIdx   int
	Calls     []fantasy.Call
	mu        sync.Mutex
}

// MockResponse defines what the model returns for one call.
type MockResponse struct {
	Text      string
	ToolCalls []MockToolCall
	Usage     fantasy.Usage
}

// MockToolCall is a tool call the mock model emits.
type MockToolCall struct {
	Name  string
	ID    string
	Input any
}

func (m *MockModel) Generate(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, call)
	m.mu.Unlock()
	return m.nextResponse(), nil
}

func (m *MockModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, call)
	m.mu.Unlock()
	resp := m.nextResponse()

	return func(yield func(fantasy.StreamPart) bool) {
		// Emit tool calls
		for _, c := range resp.Content {
			if c.GetType() == fantasy.ContentTypeToolCall {
				tc, _ := fantasy.AsContentType[fantasy.ToolCallContent](c)
				if !yield(fantasy.StreamPart{
					Type:          fantasy.StreamPartTypeToolCall,
					ID:            tc.ToolCallID,
					ToolCallName:  tc.ToolName,
					ToolCallInput: tc.Input,
				}) {
					return
				}
			}
		}

		// Emit text
		text := resp.Content.Text()
		if text != "" {
			if !yield(fantasy.StreamPart{
				Type:  fantasy.StreamPartTypeTextDelta,
				ID:    "text-0",
				Delta: text,
			}) {
				return
			}
		}

		// Finish
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: resp.FinishReason,
			Usage:        resp.Usage,
		})
	}, nil
}

func (m *MockModel) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (m *MockModel) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

func (m *MockModel) Provider() string { return "mock" }
func (m *MockModel) Model() string    { return "mock-model" }

// GetCalls returns a snapshot of all recorded calls (thread-safe).
func (m *MockModel) GetCalls() []fantasy.Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]fantasy.Call, len(m.Calls))
	copy(cp, m.Calls)
	return cp
}

func (m *MockModel) nextResponse() *fantasy.Response {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.Responses) {
		return &fantasy.Response{
			Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "done"}},
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		}
	}

	mr := m.Responses[m.callIdx]
	m.callIdx++

	var content fantasy.ResponseContent

	for _, tc := range mr.ToolCalls {
		inputJSON, _ := json.Marshal(tc.Input)
		content = append(content, fantasy.ToolCallContent{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Input:      string(inputJSON),
		})
	}

	if mr.Text != "" {
		content = append(content, fantasy.TextContent{Text: mr.Text})
	}

	finishReason := fantasy.FinishReasonStop
	if len(mr.ToolCalls) > 0 {
		finishReason = fantasy.FinishReasonToolCalls
	}

	return &fantasy.Response{
		Content:      content,
		FinishReason: finishReason,
		Usage:        mr.Usage,
	}
}
