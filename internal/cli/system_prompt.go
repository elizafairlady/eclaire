package cli

import (
	"fmt"
	"os"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/spf13/cobra"
)

var (
	spAgent string
	spMode  string
)

var systemPromptCmd = &cobra.Command{
	Use:   "system-prompt",
	Short: "Print the assembled system prompt for an agent",
	Long:  "Assembles and prints the effective system prompt without requiring a running daemon.",
	RunE:  runSystemPrompt,
}

func init() {
	systemPromptCmd.Flags().StringVarP(&spAgent, "agent", "a", "orchestrator", "agent ID")
	systemPromptCmd.Flags().StringVarP(&spMode, "mode", "m", "full", "prompt mode: full, minimal, none")
}

func runSystemPrompt(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Find agent from builtins + disk
	var target agent.Agent
	for _, a := range agent.BuiltinAgents() {
		if a.ID() == spAgent {
			target = a
			break
		}
	}
	if target == nil {
		// Try loading from disk
		diskAgents, err := agent.LoadAgentsDir(cfg.AgentsDir())
		if err == nil {
			for _, a := range diskAgents {
				if a.ID() == spAgent {
					target = a
					break
				}
			}
		}
	}
	if target == nil {
		return fmt.Errorf("agent %q not found", spAgent)
	}

	// Get embedded workspace
	var embedded map[string]string
	if ba, ok := target.(interface{ EmbeddedWorkspace() map[string]string }); ok {
		embedded = ba.EmbeddedWorkspace()
	}

	// Build workspace loader + context engine
	cwd, _ := os.Getwd()
	workspaces := agent.NewWorkspaceLoader(
		cfg.WorkspaceDir(),
		cfg.AgentsDir(),
		cwd,
	)
	skillLoader := agent.NewSkillLoader(
		cfg.SkillsDir(),
		cfg.AgentsDir(),
		cwd,
	)
	engine := agent.NewContextEngine(nil, workspaces, skillLoader)

	// Build registry for dynamic agent listing in prompt
	reg := agent.NewRegistry()
	for _, a := range agent.BuiltinAgents() {
		reg.Register(a)
	}
	if diskAgents, err := agent.LoadAgentsDir(cfg.AgentsDir()); err == nil {
		for _, a := range diskAgents {
			reg.Register(a)
		}
	}
	engine.SetRegistry(reg)

	// Load workspace
	ws, err := workspaces.Load(spAgent, embedded)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	// Resolve mode
	mode := agent.PromptMode(spMode)

	// Assemble
	var skillsAllowlist []string
	if sa, ok := target.(interface{ SkillsAllowlist() []string }); ok {
		skillsAllowlist = sa.SkillsAllowlist()
	}
	var sectionFeatures []agent.SectionFeature
	if sf, ok := target.(agent.SectionFeatured); ok {
		sectionFeatures = sf.SectionFeatures()
	}
	plan := engine.Assemble(spAgent, ws, target.RequiredTools(), 128000, "", mode, skillsAllowlist, sectionFeatures, nil)

	// Print prompt to stdout
	fmt.Println(plan.SystemPrompt)

	// Print summary to stderr
	fmt.Fprintf(os.Stderr, "\n--- Summary ---\n")
	fmt.Fprintf(os.Stderr, "Agent: %s\n", spAgent)
	fmt.Fprintf(os.Stderr, "Mode: %s\n", mode)
	fmt.Fprintf(os.Stderr, "Sections: ")
	for i, s := range plan.Sections {
		if i > 0 {
			fmt.Fprintf(os.Stderr, ", ")
		}
		fmt.Fprintf(os.Stderr, "%s(%d)", s.Name, s.Priority)
	}
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Estimated tokens: %d\n", plan.Budget.SystemPromptEst)
	fmt.Fprintf(os.Stderr, "Context window: %d\n", plan.Budget.ContextWindow)
	fmt.Fprintf(os.Stderr, "Available: %d\n", plan.Budget.Available)

	return nil
}
