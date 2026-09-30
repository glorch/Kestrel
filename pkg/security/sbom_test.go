package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSBOMFromGoMod(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kestrel-sbom-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	goModContent := `module example.com/my-app

go 1.23

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/spf13/cobra v1.8.0
)

require github.com/fatih/color v1.16.0
`
	goModPath := filepath.Join(tempDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	sbom, err := GenerateSBOMFromGoMod("my-app", "v1.0.0", goModPath)
	if err != nil {
		t.Fatalf("failed to generate sbom: %v", err)
	}

	if sbom.BOMFormat != "CycloneDX" || sbom.SpecVersion != "1.5" {
		t.Errorf("unexpected format: %s/%s", sbom.BOMFormat, sbom.SpecVersion)
	}

	if len(sbom.Components) != 3 {
		t.Fatalf("expected 3 components, got %d", len(sbom.Components))
	}

	comp1 := sbom.Components[0]
	if comp1.Name != "github.com/gin-gonic/gin" || comp1.Version != "v1.9.1" {
		t.Errorf("unexpected component 1: %+v", comp1)
	}

	// Verify JSON export
	jsonBytes, err := sbom.ExportJSON()
	if err != nil || len(jsonBytes) == 0 {
		t.Fatalf("failed to export json: %v", err)
	}
}

func TestEvaluateSecurityGate(t *testing.T) {
	policy := SecurityGatePolicy{
		MaxCriticalAllowed: 0,
		MaxHighAllowed:     1,
	}

	// Case 1: Passes (1 high, 0 critical)
	vulnsPass := []Vulnerability{
		{ID: "CVE-2026-1", Severity: "HIGH", Package: "foo"},
		{ID: "CVE-2026-2", Severity: "LOW", Package: "bar"},
	}
	if err := EvaluateSecurityGate(vulnsPass, policy); err != nil {
		t.Errorf("expected security gate to pass, got: %v", err)
	}

	// Case 2: Fails due to CRITICAL
	vulnsFailCritical := []Vulnerability{
		{ID: "CVE-2026-9", Severity: "CRITICAL", Package: "kernel"},
	}
	if err := EvaluateSecurityGate(vulnsFailCritical, policy); err == nil {
		t.Error("expected security gate to fail on CRITICAL vuln")
	}

	// Case 3: Fails due to exceeding max HIGH
	vulnsFailHigh := []Vulnerability{
		{ID: "CVE-2026-3", Severity: "HIGH", Package: "a"},
		{ID: "CVE-2026-4", Severity: "HIGH", Package: "b"},
	}
	if err := EvaluateSecurityGate(vulnsFailHigh, policy); err == nil {
		t.Error("expected security gate to fail when HIGH > 1")
	}
}
