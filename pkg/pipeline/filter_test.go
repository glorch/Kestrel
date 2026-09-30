package pipeline

import (
	"testing"
)

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		expect  bool
	}{
		{"services/order/main.go", "services/order/main.go", true},
		{"services/order/main.go", "services/user/main.go", false},
		{"services/**", "services/order/api/handler.go", true},
		{"services/**", "docs/index.md", false},
		{"**/*.md", "docs/architecture/design.md", true},
		{"**/*.md", "services/order/main.go", false},
		{"services/*/cmd", "services/order/cmd", true},
		{"services/*/cmd", "services/order/sub/cmd", false},
	}

	for _, tt := range tests {
		res := MatchPath(tt.pattern, tt.path)
		if res != tt.expect {
			t.Errorf("MatchPath(%q, %q) = %v; want %v", tt.pattern, tt.path, res, tt.expect)
		}
	}
}

func TestShouldRunForPaths(t *testing.T) {
	patterns := []string{
		"services/order/**",
		"!services/order/**/*.md",
	}

	// 1. Changes only in docs within order service -> should be excluded
	changedDocs := []string{"services/order/README.md"}
	if ShouldRunForPaths(patterns, changedDocs) {
		t.Error("expected markdown doc change to be excluded by !*.md")
	}

	// 2. Changes in Go code within order service -> should run
	changedCode := []string{"services/order/api/handler.go"}
	if !ShouldRunForPaths(patterns, changedCode) {
		t.Error("expected Go code change in services/order to trigger run")
	}

	// 3. Changes in user service -> should NOT run for order service
	changedOther := []string{"services/user/main.go"}
	if ShouldRunForPaths(patterns, changedOther) {
		t.Error("expected services/user change to NOT trigger order service")
	}

	// 4. Empty patterns -> runs by default
	if !ShouldRunForPaths(nil, changedCode) {
		t.Error("expected empty patterns to run by default")
	}

	// 5. Empty changed files -> runs by default (no change detection info available)
	if !ShouldRunForPaths(patterns, nil) {
		t.Error("expected empty changed list to run by default")
	}
}
