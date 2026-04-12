package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a project directory for eclaire",
	Long:  "Creates a .eclaire/ directory in the current working directory with project-level workspace and config scaffolding.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	home, _ := os.UserHomeDir()
	if cwd == home {
		return fmt.Errorf("cannot init project in home directory")
	}

	eclaireDir := filepath.Join(cwd, ".eclaire")
	if info, err := os.Stat(eclaireDir); err == nil && info.IsDir() {
		fmt.Fprintf(cmd.OutOrStdout(), "Project already initialized: %s\n", eclaireDir)
		return nil
	}

	if err := initProjectDir(eclaireDir); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Initialized project: %s\n", eclaireDir)
	return nil
}

// initProjectDir creates the .eclaire/ directory structure for a project.
func initProjectDir(eclaireDir string) error {
	dirs := []string{
		eclaireDir,
		filepath.Join(eclaireDir, "workspace"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Create a minimal config.yaml if it doesn't exist
	configPath := filepath.Join(eclaireDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		content := "# Project-level eclaire config.\n# Values here override ~/.eclaire/config.yaml for this project.\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}

	return nil
}
