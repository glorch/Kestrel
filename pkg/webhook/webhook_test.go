package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	secret := "kestrel-secret-key"
	payload := []byte(`{"message": "ping"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(secret, payload, validSig) {
		t.Error("expected valid signature to pass verification")
	}

	if VerifySignature(secret, payload, "sha256=invalidhash123") {
		t.Error("expected invalid signature to fail verification")
	}
}

func TestFilterRules(t *testing.T) {
	rules := &FilterRules{
		Branches:    []string{"main", "release/*"},
		Paths:       []string{"pkg/*", "cmd/*"},
		PathsIgnore: []string{"*.md", "docs/*"},
	}

	// 1. Matched event
	eventMatch := &GitEvent{
		Branch:        "main",
		ModifiedFiles: []string{"pkg/engine/engine.go", "README.md"},
	}
	if !Matches(eventMatch, rules) {
		t.Error("expected event to match rules")
	}

	// 2. Mismatched branch
	eventBadBranch := &GitEvent{
		Branch:        "feature/my-branch",
		ModifiedFiles: []string{"pkg/engine/engine.go"},
	}
	if Matches(eventBadBranch, rules) {
		t.Error("expected event with feature/my-branch to be rejected")
	}

	// 3. Only ignored files changed
	eventDocsOnly := &GitEvent{
		Branch:        "main",
		ModifiedFiles: []string{"README.md", "docs/architecture.md"},
	}
	if Matches(eventDocsOnly, rules) {
		t.Error("expected event with only ignored files to be rejected")
	}
}

func TestParseGitHubPush(t *testing.T) {
	payload := `{
		"ref": "refs/heads/main",
		"repository": {
			"full_name": "glorch/Kestrel"
		},
		"head_commit": {
			"id": "commit123456",
			"author": {
				"username": "glorch"
			},
			"modified": ["pkg/pipeline/model.go"]
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload))
	req.Header.Set("X-GitHub-Event", "push")

	event, err := ParseRequest(req, "")
	if err != nil {
		t.Fatalf("failed to parse github request: %v", err)
	}

	if event.Provider != "github" || event.Branch != "main" || event.Repo != "glorch/Kestrel" {
		t.Errorf("unexpected event: %+v", event)
	}
	if event.Commit != "commit123456" || event.Author != "glorch" {
		t.Errorf("unexpected commit/author: %+v", event)
	}
	if len(event.ModifiedFiles) != 1 || event.ModifiedFiles[0] != "pkg/pipeline/model.go" {
		t.Errorf("unexpected modified files: %v", event.ModifiedFiles)
	}
}
