package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/glorch/kestrel/pkg/cd"
	"github.com/glorch/kestrel/pkg/dag"
	"github.com/glorch/kestrel/pkg/engine"
	"github.com/glorch/kestrel/pkg/logger"
	"github.com/glorch/kestrel/pkg/notify"
	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/runner"
	"github.com/glorch/kestrel/pkg/security"
	"github.com/glorch/kestrel/pkg/server"
	"github.com/glorch/kestrel/pkg/store"
	"github.com/glorch/kestrel/pkg/version"
)

var (
	configFile    string
	dryRun        bool
	forceExecutor string
	workDir       string
	graphFormat   string

	// Server flags
	serverPort    int
	serverStore   string
	webhookSecret string

	// Runner flags
	runnerServerURL string
	runnerID        string
	runnerTags      string
	runnerCapacity  int

	// SBOM flags
	sbomOutFile string

	// Approval flags
	approvalApprover string
	approvalComment  string

	// Freeze flags
	freezeServerURL   string
	freezeID          string
	freezeName        string
	freezeEnv         string
	freezeDays        int
	freezeBypassToken string

	// Notify flags
	notifyChannelType string
	notifyWebhook     string
	notifySecret      string
	notifyTitle       string
	notifyContent     string
	notifyType        string
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
		Short: "Execute a local pipeline",
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

			fs, err := store.NewFileStore(".kestrel/store")
			var st store.Store = store.NewMemoryStore()
			if err == nil {
				st = fs
			}

			eng := engine.New(p, engine.Options{
				WorkDir:       workDir,
				ForceExecutor: forceExecutor,
				DryRun:        dryRun,
				Store:         st,
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

	// --- SERVER COMMAND ---
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Kestrel distributed control plane server",
	}

	serverStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start Kestrel central control plane server",
		RunE: func(cmd *cobra.Command, args []string) error {
			fs, err := store.NewFileStore(serverStore)
			if err != nil {
				return fmt.Errorf("failed to initialize store: %w", err)
			}

			srv := server.NewServer(fs)
			if webhookSecret != "" {
				srv.SetWebhookSecret(webhookSecret)
			}

			addr := fmt.Sprintf(":%d", serverPort)
			httpServer := &http.Server{
				Addr:    addr,
				Handler: srv.HTTPHandler(),
			}

			fmt.Println(color.CyanString("🦅 Kestrel Central Server starting on %s...", addr))
			fmt.Println(color.WhiteString("  • RPC Endpoint: http://localhost:%d/rpc/", serverPort))
			fmt.Println(color.WhiteString("  • REST API:     http://localhost:%d/api/v1/", serverPort))
			fmt.Println(color.WhiteString("  • Webhook:      http://localhost:%d/webhook", serverPort))

			// Graceful shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

			go func() {
				<-sigCh
				fmt.Println("\nShutting down server...")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(ctx)
			}()

			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	serverStartCmd.Flags().IntVarP(&serverPort, "port", "p", 8080, "HTTP/RPC listen port")
	serverStartCmd.Flags().StringVar(&serverStore, "store-dir", ".kestrel/store", "Persistent data store directory")
	serverStartCmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "Shared secret for Git webhook signature validation")
	serverCmd.AddCommand(serverStartCmd)

	// --- RUNNER COMMAND ---
	runnerCmd := &cobra.Command{
		Use:   "runner",
		Short: "Manage Kestrel distributed worker runner agent",
	}

	runnerStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start runner daemon and connect to central server",
		RunE: func(cmd *cobra.Command, args []string) error {
			tags := strings.Split(runnerTags, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}

			daemon := runner.NewDaemon(runner.Config{
				ID:        runnerID,
				ServerURL: runnerServerURL,
				Tags:      tags,
				Capacity:  runnerCapacity,
			})

			fmt.Println(color.CyanString("🏃 Kestrel Runner connecting to %s...", runnerServerURL))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

			go func() {
				<-sigCh
				fmt.Println("\nStopping runner...")
				daemon.Stop()
				cancel()
			}()

			return daemon.Start(ctx)
		},
	}
	runnerStartCmd.Flags().StringVarP(&runnerServerURL, "server", "s", "http://localhost:8080", "Kestrel server address")
	runnerStartCmd.Flags().StringVar(&runnerID, "id", "", "Custom runner ID (default: auto-generated)")
	runnerStartCmd.Flags().StringVar(&runnerTags, "tags", "host,docker", "Comma-separated runner tags")
	runnerStartCmd.Flags().IntVarP(&runnerCapacity, "capacity", "c", 2, "Maximum concurrent jobs")
	runnerCmd.AddCommand(runnerStartCmd)

	// --- SBOM COMMAND ---
	sbomCmd := &cobra.Command{
		Use:   "sbom [path_to_go_mod]",
		Short: "Generate CycloneDX 1.5 Software Bill of Materials (SBOM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			targetMod := "go.mod"
			if len(args) > 0 {
				targetMod = args[0]
			}

			sbom, err := security.GenerateSBOMFromGoMod("kestrel-app", "v0.2.0", targetMod)
			if err != nil {
				return err
			}

			jsonBytes, err := sbom.ExportJSON()
			if err != nil {
				return err
			}

			if sbomOutFile != "" {
				if err := os.WriteFile(sbomOutFile, jsonBytes, 0644); err != nil {
					return err
				}
				fmt.Println(color.GreenString("✔ CycloneDX SBOM saved to %s (%d components)", sbomOutFile, len(sbom.Components)))
			} else {
				fmt.Println(string(jsonBytes))
			}
			return nil
		},
	}
	sbomCmd.Flags().StringVarP(&sbomOutFile, "out", "o", "", "Output file path (default: stdout)")

	// --- APPROVALS COMMAND ---
	approvalsCmd := &cobra.Command{
		Use:   "approvals",
		Short: "Manage CD manual approval gates",
	}

	approvalsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List pending approval gates",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(runnerServerURL + "/api/v1/approvals")
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			var gates []*cd.GateRequest
			if err := json.NewDecoder(resp.Body).Decode(&gates); err != nil {
				return err
			}

			if len(gates) == 0 {
				fmt.Println("No pending approval gates.")
				return nil
			}

			fmt.Println("Pending Approval Gates:")
			for _, g := range gates {
				fmt.Printf("  • Gate ID: %s | Run: %s | Job: %s | Env: %s | Expires: %s\n",
					color.YellowString(g.ID), g.RunID, g.JobID, g.Environment, g.ExpiresAt.Format("15:04:05"))
			}
			return nil
		},
	}
	approvalsListCmd.Flags().StringVarP(&runnerServerURL, "server", "s", "http://localhost:8080", "Kestrel server address")

	approvalsApproveCmd := &cobra.Command{
		Use:   "approve <gate_id>",
		Short: "Approve a pending gate to continue pipeline deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateID := args[0]
			payload := map[string]string{
				"gate_id":  gateID,
				"approver": approvalApprover,
				"comment":  approvalComment,
			}
			data, _ := json.Marshal(payload)
			resp, err := http.Post(runnerServerURL+"/api/v1/approvals/approve", "application/json", bytes.NewReader(data))
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				return fmt.Errorf("approval failed with HTTP %d", resp.StatusCode)
			}

			fmt.Println(color.GreenString("✔ Gate '%s' approved by %s!", gateID, approvalApprover))
			return nil
		},
	}
	approvalsApproveCmd.Flags().StringVarP(&runnerServerURL, "server", "s", "http://localhost:8080", "Kestrel server address")
	approvalsApproveCmd.Flags().StringVarP(&approvalApprover, "approver", "a", "admin", "Approver username")
	approvalsApproveCmd.Flags().StringVarP(&approvalComment, "comment", "m", "Approved via CLI", "Approval comment")

	approvalsCmd.AddCommand(approvalsListCmd, approvalsApproveCmd)

	// --- RUNS COMMAND ---
	runsCmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect pipeline execution history",
	}

	runsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded pipeline runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			fs, err := store.NewFileStore(".kestrel/store")
			if err != nil {
				return err
			}
			runs, err := fs.ListRuns(context.Background(), store.RunFilter{})
			if err != nil {
				return err
			}

			if len(runs) == 0 {
				fmt.Println("No recorded pipeline runs found in .kestrel/store")
				return nil
			}

			fmt.Println("Recent Pipeline Runs:")
			for _, r := range runs {
				statusColor := color.GreenString(r.Status)
				if r.Status == "FAILED" {
					statusColor = color.RedString(r.Status)
				}
				fmt.Printf("  • [%s] %-20s %s (%s, %s)\n",
					r.ID, r.PipelineName, statusColor, r.StartedAt.Format("2006-01-02 15:04:05"), r.Trigger)
			}
			return nil
		},
	}
	runsCmd.AddCommand(runsListCmd)

	// --- FREEZE COMMAND ---
	freezeCmd := &cobra.Command{
		Use:   "freeze",
		Short: "Manage production change freeze windows and policies",
	}

	freezeListCmd := &cobra.Command{
		Use:   "list",
		Short: "List active change freeze rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(freezeServerURL + "/api/v1/freeze/rules")
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			var rules []*cd.FreezeRule
			if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
				return err
			}

			if len(rules) == 0 {
				fmt.Println("No active change freeze rules found.")
				return nil
			}

			fmt.Println("Active Change Freeze Rules:")
			for _, r := range rules {
				fmt.Printf("  • [%s] %s | Envs: %v | Type: %s\n",
					r.ID, color.YellowString(r.Name), r.Environments, r.Type)
			}
			return nil
		},
	}
	freezeListCmd.Flags().StringVarP(&freezeServerURL, "server", "s", "http://localhost:8080", "Kestrel server address")

	freezeAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a temporary change freeze window",
		RunE: func(cmd *cobra.Command, args []string) error {
			if freezeID == "" {
				freezeID = fmt.Sprintf("freeze-%d", time.Now().UnixNano()/1e6)
			}
			if freezeDays <= 0 {
				freezeDays = 1
			}

			rule := cd.FreezeRule{
				ID:           freezeID,
				Name:         freezeName,
				Type:         cd.FreezeDateRange,
				Environments: strings.Split(freezeEnv, ","),
				StartTime:    time.Now(),
				EndTime:      time.Now().Add(time.Duration(freezeDays) * 24 * time.Hour),
				AllowBypass:  freezeBypassToken != "",
			}
			if freezeBypassToken != "" {
				rule.BypassTokens = []string{freezeBypassToken}
			}

			data, _ := json.Marshal(rule)
			resp, err := http.Post(freezeServerURL+"/api/v1/freeze/rules", "application/json", bytes.NewReader(data))
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				return fmt.Errorf("failed to add freeze rule, HTTP %d", resp.StatusCode)
			}

			fmt.Println(color.GreenString("✔ Change freeze rule '%s' (%s) registered successfully!", rule.Name, rule.ID))
			return nil
		},
	}
	freezeAddCmd.Flags().StringVarP(&freezeServerURL, "server", "s", "http://localhost:8080", "Kestrel server address")
	freezeAddCmd.Flags().StringVar(&freezeID, "id", "", "Freeze rule ID")
	freezeAddCmd.Flags().StringVarP(&freezeName, "name", "n", "Emergency Production Freeze", "Freeze rule name")
	freezeAddCmd.Flags().StringVarP(&freezeEnv, "env", "e", "production", "Target comma-separated environments")
	freezeAddCmd.Flags().IntVarP(&freezeDays, "days", "d", 2, "Freeze window duration in days")
	freezeAddCmd.Flags().StringVar(&freezeBypassToken, "bypass-token", "", "Emergency bypass secret token")

	freezeCmd.AddCommand(freezeListCmd, freezeAddCmd)

	// --- NOTIFY COMMAND ---
	notifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Send alerts via ChatOps webhooks (Feishu, DingTalk, WeCom, Slack)",
	}

	notifySendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a notification message to configured webhook",
		RunE: func(cmd *cobra.Command, args []string) error {
			if notifyWebhook == "" {
				return fmt.Errorf("missing --webhook parameter")
			}
			ch := notify.ChannelConfig{
				Name:    "cli-alert",
				Type:    notifyChannelType,
				Webhook: notifyWebhook,
				Secret:  notifySecret,
				Enabled: true,
			}
			msg := &notify.Message{
				Title:   notifyTitle,
				Content: notifyContent,
				Type:    notify.NotificationType(notifyType),
			}

			disp := notify.NewDispatcher([]notify.ChannelConfig{ch})
			results := disp.Send(context.Background(), msg)
			if len(results) > 0 && !results[0].Success {
				return fmt.Errorf("dispatch error: %s", results[0].Error)
			}

			fmt.Println(color.GreenString("✔ Notification successfully sent to %s channel!", notifyChannelType))
			return nil
		},
	}
	notifySendCmd.Flags().StringVar(&notifyChannelType, "channel", "generic", "Channel type: feishu, dingtalk, wecom, slack, generic")
	notifySendCmd.Flags().StringVarP(&notifyWebhook, "webhook", "w", "", "Target webhook URL")
	notifySendCmd.Flags().StringVar(&notifySecret, "secret", "", "Webhook signing secret (for Feishu / DingTalk)")
	notifySendCmd.Flags().StringVarP(&notifyTitle, "title", "t", "Kestrel Alert", "Message title")
	notifySendCmd.Flags().StringVarP(&notifyContent, "content", "c", "Pipeline execution event notification", "Message content body")
	notifySendCmd.Flags().StringVar(&notifyType, "type", "CUSTOM", "Event type (PIPELINE_SUCCESS, PIPELINE_FAILURE, APPROVAL_REQUEST, CUSTOM)")

	notifyCmd.AddCommand(notifySendCmd)

	// --- VERSION COMMAND ---
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print Kestrel version and build info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Info())
		},
	}

	rootCmd.AddCommand(runCmd, lintCmd, graphCmd, serverCmd, runnerCmd, sbomCmd, approvalsCmd, runsCmd, freezeCmd, notifyCmd, versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
