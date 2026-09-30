package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/security"
)

// ActionContext encapsulates the runtime parameters provided to an Action.
type ActionContext struct {
	Workspace string
	With      map[string]string
	Env       map[string]string
	Out       io.Writer
}

// Action defines the contract for reusable pipeline step components.
type Action interface {
	Name() string
	Description() string
	Execute(ctx context.Context, actCtx *ActionContext) error
}

// Registry stores and discovers reusable Actions.
type Registry struct {
	mu      sync.RWMutex
	actions map[string]Action
}

// NewRegistry initializes an empty action registry.
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]Action),
	}
}

// Register registers an action into the registry.
func (r *Registry) Register(act Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[act.Name()] = act
}

// Get looks up an action by name.
func (r *Registry) Get(name string) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	act, ok := r.actions[name]
	return act, ok
}

// DefaultRegistry is the globally available action registry with built-in actions.
var DefaultRegistry = NewRegistry()

func init() {
	DefaultRegistry.Register(&CheckoutAction{})
	DefaultRegistry.Register(&SetupGoAction{})
	DefaultRegistry.Register(&EchoAction{})
	DefaultRegistry.Register(&SBOMAction{})
}

// --- Built-in Action: actions/checkout ---
type CheckoutAction struct{}

func (a *CheckoutAction) Name() string        { return "actions/checkout" }
func (a *CheckoutAction) Description() string { return "Checks out code workspace" }

func (a *CheckoutAction) Execute(ctx context.Context, actCtx *ActionContext) error {
	dest := actCtx.Workspace
	if custom := actCtx.With["path"]; custom != "" {
		dest = filepath.Join(actCtx.Workspace, custom)
	}
	fmt.Fprintf(actCtx.Out, "✔ Verified workspace directory: %s\n", dest)
	return nil
}

// --- Built-in Action: actions/setup-go ---
type SetupGoAction struct{}

func (a *SetupGoAction) Name() string        { return "actions/setup-go" }
func (a *SetupGoAction) Description() string { return "Configures Go toolchain environment" }

func (a *SetupGoAction) Execute(ctx context.Context, actCtx *ActionContext) error {
	version := actCtx.With["version"]
	if version == "" {
		version = "1.23"
	}
	proxy := actCtx.With["goproxy"]
	if proxy != "" {
		_ = os.Setenv("GOPROXY", proxy)
		fmt.Fprintf(actCtx.Out, "Setting GOPROXY=%s\n", proxy)
	}
	fmt.Fprintf(actCtx.Out, "✔ Configured Go environment (target version: %s)\n", version)
	return nil
}

// --- Built-in Action: actions/echo ---
type EchoAction struct{}

func (a *EchoAction) Name() string        { return "actions/echo" }
func (a *EchoAction) Description() string { return "Prints formatted notice message" }

func (a *EchoAction) Execute(ctx context.Context, actCtx *ActionContext) error {
	msg := actCtx.With["message"]
	fmt.Fprintf(actCtx.Out, "📢 [Action Notice] %s\n", msg)
	return nil
}

// --- Built-in Action: actions/sbom ---
type SBOMAction struct{}

func (a *SBOMAction) Name() string        { return "actions/sbom" }
func (a *SBOMAction) Description() string { return "Generates CycloneDX 1.5 SBOM for workspace" }

func (a *SBOMAction) Execute(ctx context.Context, actCtx *ActionContext) error {
	outFile := actCtx.With["out"]
	if outFile == "" {
		outFile = ".kestrel/sbom.json"
	}
	targetDir := filepath.Dir(outFile)
	_ = os.MkdirAll(targetDir, 0755)

	goModPath := filepath.Join(actCtx.Workspace, "go.mod")
	sbom, err := security.GenerateSBOMFromGoMod("kestrel-project", "latest", goModPath)
	if err != nil {
		return fmt.Errorf("failed to generate sbom: %w", err)
	}

	data, err := sbom.ExportJSON()
	if err != nil {
		return err
	}

	if err := os.WriteFile(outFile, data, 0644); err != nil {
		return err
	}

	fmt.Fprintf(actCtx.Out, "✔ Generated CycloneDX SBOM (%d components) at %s\n", len(sbom.Components), outFile)
	return nil
}

// ExecuteStepAction checks if a step uses an action, and if so executes it via the registry.
func ExecuteStepAction(ctx context.Context, step *pipeline.Step, actCtx *ActionContext) (bool, error) {
	if step.Uses == "" {
		return false, nil
	}

	act, ok := DefaultRegistry.Get(step.Uses)
	if !ok {
		return true, fmt.Errorf("unknown action '%s'", step.Uses)
	}

	actCtx.With = step.With
	err := act.Execute(ctx, actCtx)
	return true, err
}
