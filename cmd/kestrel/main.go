package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/glorch/kestrel/pkg/dag"
	"github.com/glorch/kestrel/pkg/engine"
	"github.com/glorch/kestrel/pkg/logger"
	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/version"
)

var (
	configFile    string
	dryRun        bool
	forceExecutor string
	workDir       string
	graphFormat   string
)

func findConfigFile(specified string) (string, error) {
	if specified != "" {
		if _, err := os.Stat(specified); err == nil {
			return specified, nil
		}
		return "", fmt.Errorf("specified pipeline configuration file '%s' does not exist", specified)
	}

	candidates := []string{".kestrel.yaml", ".kestrel.yml", "kestrel.yaml", "kestrel.yml"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("no kestrel configuration file found (looked for %v)", candidates)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "kestrel",
		Short: "Kestrel: A fast, lightweight cloud-native CI/CD engine in Go",
		Long: color.CyanString(`
  _  __          _             _ 
 | |/ /___  ___ | |_ _ __  ___| |
 | ' // _ \/ __|| __| '__|/ _ \ |
 | . \  __/\__ \| |_| |  |  __/ |
 |_|\_\___||___/ \__|_|   \___|_|
 
 A lightweight, razor-sharp CI/CD engine built with Go.
`),
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "file", "f", "", "Pipeline configuration file path (default: .kestrel.yaml)")

	// --- RUN COMMAND ---
	runCmd := &cobra.Command{
		Use:   "run [path]",
		Short: "Execute a local or remote pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := configFile
			if len(args) > 0 {
				target = args[0]
			}

			path, err := findConfigFile(target)
			if err != nil {
				return err
			}

			p, err := pipeline.ParseFile(path)
			if err != nil {
				return fmt.Errorf("configuration error: %w", err)
			}

			eng := engine.New(p, engine.Options{
				WorkDir:       workDir,
				ForceExecutor: forceExecutor,
				DryRun:        dryRun,
			}, logger.Default())

			return eng.Run(context.Background())
		},
	}
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate pipeline execution without running steps")
	runCmd.Flags().StringVar(&forceExecutor, "executor", "", "Force executor override ('docker' or 'host')")
	runCmd.Flags().StringVarP(&workDir, "workdir", "w", "", "Workspace root directory (defaults to current directory)")

	// --- LINT COMMAND ---
	lintCmd := &cobra.Command{
		Use:   "lint [path]",
		Short: "Validate pipeline configuration and detect circular dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := configFile
			if len(args) > 0 {
				target = args[0]
			}

			path, err := findConfigFile(target)
			if err != nil {
				return err
			}

			p, err := pipeline.ParseFile(path)
			if err != nil {
				return fmt.Errorf("lint error: %w", err)
			}

			// Validate DAG
			g, err := dag.BuildGraph(p.Jobs)
			if err != nil {
				return fmt.Errorf("DAG topology error: %w", err)
			}

			batches, err := g.ResolveBatches()
			if err != nil {
				return fmt.Errorf("dependency error: %w", err)
			}

			fmt.Println(color.GreenString("✔ Configuration '%s' is valid!", path))
			fmt.Printf("  • Total Jobs: %d\n", len(p.Jobs))
			fmt.Printf("  • Execution Stages: %d\n", len(batches))
			return nil
		},
	}

	// --- GRAPH COMMAND ---
	graphCmd := &cobra.Command{
		Use:   "graph [path]",
		Short: "Print visual dependency graph (ASCII or Mermaid)",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := configFile
			if len(args) > 0 {
				target = args[0]
			}

			path, err := findConfigFile(target)
			if err != nil {
				return err
			}

			p, err := pipeline.ParseFile(path)
			if err != nil {
				return err
			}

			g, err := dag.BuildGraph(p.Jobs)
			if err != nil {
				return err
			}

			if graphFormat == "mermaid" {
				fmt.Print(g.ToMermaid())
			} else {
				ascii, err := g.ToASCII()
				if err != nil {
					return err
				}
				fmt.Print(ascii)
			}
			return nil
		},
	}
	graphCmd.Flags().StringVarP(&graphFormat, "format", "m", "ascii", "Graph output format: 'ascii' or 'mermaid'")

	// --- VERSION COMMAND ---
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print Kestrel version and build info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Info())
		},
	}

	rootCmd.AddCommand(runCmd, lintCmd, graphCmd, versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
