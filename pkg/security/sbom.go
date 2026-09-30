package security

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Component represents a software dependency inside the SBOM.
type Component struct {
	Type    string `json:"type"` // library, framework, container
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl,omitempty"` // Package URL (e.g. pkg:golang/github.com/gin-gonic/gin@v1.9.1)
	Scope   string `json:"scope,omitempty"`
}

// SBOM represents a CycloneDX-compatible Software Bill of Materials.
type SBOM struct {
	BOMFormat   string      `json:"bomFormat"`
	SpecVersion string      `json:"specVersion"`
	SerialNumber string     `json:"serialNumber"`
	Version     int         `json:"version"`
	Metadata    Metadata    `json:"metadata"`
	Components  []Component `json:"components"`
}

// Metadata holds project information within the SBOM.
type Metadata struct {
	Timestamp string    `json:"timestamp"`
	Tool      string    `json:"tool"`
	Component Component `json:"component"`
}

// GenerateSBOMFromGoMod parses a go.mod file and produces a standardized CycloneDX SBOM.
func GenerateSBOMFromGoMod(projectName, projectVersion, goModPath string) (*SBOM, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open go.mod: %w", err)
	}
	defer file.Close()

	components := make([]Component, 0)
	scanner := bufio.NewScanner(file)
	inRequireBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}

		var modName, modVer string
		if inRequireBlock {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modName = parts[0]
				modVer = parts[1]
			}
		} else if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				modName = parts[1]
				modVer = parts[2]
			}
		}

		if modName != "" && modVer != "" {
			components = append(components, Component{
				Type:    "library",
				Name:    modName,
				Version: modVer,
				PURL:    fmt.Sprintf("pkg:golang/%s@%s", modName, modVer),
				Scope:   "required",
			})
		}
	}

	sbom := &SBOM{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.5",
		SerialNumber: fmt.Sprintf("urn:uuid:kestrel-%d", time.Now().UnixNano()),
		Version:      1,
		Metadata: Metadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Tool:      "Kestrel DevSecOps Engine v0.2.0",
			Component: Component{
				Type:    "application",
				Name:    projectName,
				Version: projectVersion,
			},
		},
		Components: components,
	}

	return sbom, nil
}

// Vulnerability represents an identified CVE/vulnerability.
type Vulnerability struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // CRITICAL, HIGH, MEDIUM, LOW
	Package     string `json:"package"`
	Title       string `json:"title"`
	FixedIn     string `json:"fixed_in,omitempty"`
}

// SecurityGatePolicy defines thresholds for blocking pipeline execution.
type SecurityGatePolicy struct {
	MaxCriticalAllowed int `json:"max_critical_allowed"`
	MaxHighAllowed     int `json:"max_high_allowed"`
}

// EvaluateSecurityGate evaluates vulnerabilities against the security policy.
// Returns an error if the policy threshold is breached.
func EvaluateSecurityGate(vulns []Vulnerability, policy SecurityGatePolicy) error {
	criticalCount := 0
	highCount := 0

	for _, v := range vulns {
		switch strings.ToUpper(v.Severity) {
		case "CRITICAL":
			criticalCount++
		case "HIGH":
			highCount++
		}
	}

	if criticalCount > policy.MaxCriticalAllowed {
		return fmt.Errorf("security gate breached: found %d CRITICAL vulnerabilities (allowed: %d)",
			criticalCount, policy.MaxCriticalAllowed)
	}

	if highCount > policy.MaxHighAllowed {
		return fmt.Errorf("security gate breached: found %d HIGH vulnerabilities (allowed: %d)",
			highCount, policy.MaxHighAllowed)
	}

	return nil
}

// ExportJSON exports the SBOM as a formatted JSON byte slice.
func (s *SBOM) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}
