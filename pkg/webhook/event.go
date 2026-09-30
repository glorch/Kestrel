package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
)

// GitEvent captures standard information parsed from Git webhook payloads.
type GitEvent struct {
	Provider      string   `json:"provider"` // github, gitlab, gitea
	Type          string   `json:"type"`     // push, pull_request, tag
	Repo          string   `json:"repo"`
	Branch        string   `json:"branch"`
	Tag           string   `json:"tag,omitempty"`
	Commit        string   `json:"commit"`
	Author        string   `json:"author"`
	ModifiedFiles []string `json:"modified_files"`
}

// FilterRules defines criteria for deciding if a pipeline should run on a GitEvent.
type FilterRules struct {
	Branches    []string `yaml:"branches,omitempty" json:"branches,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Paths       []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	PathsIgnore []string `yaml:"paths-ignore,omitempty" json:"paths_ignore,omitempty"`
}

// VerifySignature validates an HMAC-SHA256 signature (e.g. GitHub X-Hub-Signature-256).
func VerifySignature(secret string, payload []byte, signatureHeader string) bool {
	if secret == "" {
		return true // If no secret configured, skip verification
	}

	sig := strings.TrimPrefix(signatureHeader, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expectedMAC))
}

// Matches evaluates whether the GitEvent satisfies the FilterRules.
func Matches(event *GitEvent, rules *FilterRules) bool {
	if rules == nil {
		return true
	}

	// 1. Branch check
	if len(rules.Branches) > 0 && event.Branch != "" {
		matched := false
		for _, pattern := range rules.Branches {
			if matchPattern(pattern, event.Branch) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 2. Tag check
	if len(rules.Tags) > 0 {
		if event.Tag == "" {
			return false // event is not a tag push
		}
		matched := false
		for _, pattern := range rules.Tags {
			if matchPattern(pattern, event.Tag) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 3. Path checking
	if len(event.ModifiedFiles) > 0 {
		// If paths-ignore is set, and ALL modified files match paths-ignore, don't run
		if len(rules.PathsIgnore) > 0 {
			allIgnored := true
			for _, file := range event.ModifiedFiles {
				fileIgnored := false
				for _, pattern := range rules.PathsIgnore {
					if matchPattern(pattern, file) {
						fileIgnored = true
						break
					}
				}
				if !fileIgnored {
					allIgnored = false
					break
				}
			}
			if allIgnored {
				return false
			}
		}

		// If paths is set, at least ONE modified file must match paths
		if len(rules.Paths) > 0 {
			anyMatched := false
			for _, file := range event.ModifiedFiles {
				for _, pattern := range rules.Paths {
					if matchPattern(pattern, file) {
						anyMatched = true
						break
					}
				}
				if anyMatched {
					break
				}
			}
			if !anyMatched {
				return false
			}
		}
	}

	return true
}

func matchPattern(pattern, val string) bool {
	if pattern == "*" || pattern == val {
		return true
	}
	matched, err := filepath.Match(pattern, val)
	if err == nil && matched {
		return true
	}
	// Prefix wildcard check, e.g. "release/*"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(val, prefix+"/")
	}
	return false
}
