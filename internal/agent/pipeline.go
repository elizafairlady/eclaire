package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/elizafairlady/eclaire/internal/bus"
)

// PipelineStep defines a single step in a composable pipeline.
type PipelineStep struct {
	AgentID    string `yaml:"agent_id" json:"agent_id"`
	Prompt     string `yaml:"prompt" json:"prompt"`
	InputFrom  string `yaml:"input_from,omitempty" json:"input_from,omitempty"` // step name to read output from
	Name       string `yaml:"name,omitempty" json:"name,omitempty"`
}

// Pipeline is a composable chain of agent steps.
type Pipeline struct {
	Name  string         `yaml:"name" json:"name"`
	Steps []PipelineStep `yaml:"steps" json:"steps"`
}

// PipelineResult holds the output of each step.
type PipelineResult struct {
	StepResults map[string]string `json:"step_results"`
	FinalOutput string            `json:"final_output"`
}

// PipelineRunner executes pipelines.
type PipelineRunner struct {
	registry *Registry
	bus      *bus.Bus
	logger   *slog.Logger
}

// NewPipelineRunner creates a pipeline runner.
func NewPipelineRunner(registry *Registry, msgBus *bus.Bus, logger *slog.Logger) *PipelineRunner {
	return &PipelineRunner{
		registry: registry,
		bus:      msgBus,
		logger:   logger,
	}
}

// Run executes a pipeline, passing output from each step to the next.
func (r *PipelineRunner) Run(ctx context.Context, pipeline Pipeline) (*PipelineResult, error) {
	result := &PipelineResult{
		StepResults: make(map[string]string),
	}

	for i, step := range pipeline.Steps {
		stepName := step.Name
		if stepName == "" {
			stepName = fmt.Sprintf("step-%d", i)
		}

		agent, ok := r.registry.Get(step.AgentID)
		if !ok {
			return nil, fmt.Errorf("step %s: agent %q not found", stepName, step.AgentID)
		}

		// Build the prompt, optionally incorporating previous step output
		prompt := step.Prompt
		if step.InputFrom != "" {
			prevOutput, ok := result.StepResults[step.InputFrom]
			if !ok {
				return nil, fmt.Errorf("step %s: input_from %q not found in previous results", stepName, step.InputFrom)
			}
			prompt = fmt.Sprintf("%s\n\nContext from previous step:\n%s", prompt, prevOutput)
		}

		r.logger.Info("pipeline step starting",
			"pipeline", pipeline.Name,
			"step", stepName,
			"agent", step.AgentID,
		)

		r.bus.Publish(bus.TopicAgentStarted, bus.AgentEvent{
			AgentID: step.AgentID,
			Name:    agent.Name(),
			Status:  "pipeline:" + stepName,
		})

		resp, err := agent.Handle(ctx, Request{
			Prompt: prompt,
		})
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", stepName, err)
		}

		result.StepResults[stepName] = resp.Content
		r.logger.Info("pipeline step completed",
			"pipeline", pipeline.Name,
			"step", stepName,
			"output_len", len(resp.Content),
		)
	}

	// Final output is the last step's output
	if len(pipeline.Steps) > 0 {
		lastStep := pipeline.Steps[len(pipeline.Steps)-1]
		lastName := lastStep.Name
		if lastName == "" {
			lastName = fmt.Sprintf("step-%d", len(pipeline.Steps)-1)
		}
		result.FinalOutput = result.StepResults[lastName]
	}

	return result, nil
}
