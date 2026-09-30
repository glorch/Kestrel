package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ParseRequest parses an incoming Git webhook HTTP request into a normalized GitEvent.
func ParseRequest(r *http.Request, secret string) (*GitEvent, error) {
	bodyBuf := make([]byte, 1024*1024) // up to 1MB
	n, err := r.Body.Read(bodyBuf)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	payload := bodyBuf[:n]

	// 1. Determine provider & event type from headers
	ghEvent := r.Header.Get("X-GitHub-Event")
	glEvent := r.Header.Get("X-Gitlab-Event")

	if ghEvent != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if secret != "" && !VerifySignature(secret, payload, sig) {
			return nil, ErrInvalidSignature
		}
		return parseGitHubEvent(ghEvent, payload)
	} else if glEvent != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if secret != "" && token != secret {
			return nil, ErrInvalidSignature
		}
		return parseGitLabEvent(glEvent, payload)
	}

	// Fallback generic json
	var event GitEvent
	if err := json.Unmarshal(payload, &event); err == nil && event.Repo != "" {
		if event.Provider == "" {
			event.Provider = "generic"
		}
		return &event, nil
	}

	return nil, fmt.Errorf("unrecognized webhook payload or missing provider header")
}

func parseGitHubEvent(eventType string, data []byte) (*GitEvent, error) {
	var raw struct {
		Ref        string `json:"ref"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		HeadCommit struct {
			ID     string `json:"id"`
			Author struct {
				Username string `json:"username"`
			} `json:"author"`
			Added    []string `json:"added"`
			Removed  []string `json:"removed"`
			Modified []string `json:"modified"`
		} `json:"head_commit"`
		PullRequest struct {
			Head struct {
				Ref string `json:"ref"`
				Sha string `json:"sha"`
			} `json:"head"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"pull_request"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	event := &GitEvent{
		Provider: "github",
		Type:     eventType,
		Repo:     raw.Repository.FullName,
	}

	if eventType == "push" {
		if strings.HasPrefix(raw.Ref, "refs/heads/") {
			event.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
		} else if strings.HasPrefix(raw.Ref, "refs/tags/") {
			event.Tag = strings.TrimPrefix(raw.Ref, "refs/tags/")
		}
		event.Commit = raw.HeadCommit.ID
		event.Author = raw.HeadCommit.Author.Username

		// Aggregate modified files
		var files []string
		files = append(files, raw.HeadCommit.Added...)
		files = append(files, raw.HeadCommit.Modified...)
		files = append(files, raw.HeadCommit.Removed...)
		event.ModifiedFiles = files
	} else if eventType == "pull_request" {
		event.Branch = raw.PullRequest.Head.Ref
		event.Commit = raw.PullRequest.Head.Sha
		event.Author = raw.PullRequest.User.Login
	}

	return event, nil
}

func parseGitLabEvent(eventType string, data []byte) (*GitEvent, error) {
	var raw struct {
		Ref     string `json:"ref"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		CheckoutSha string `json:"checkout_sha"`
		UserName    string `json:"user_username"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	event := &GitEvent{
		Provider: "gitlab",
		Type:     eventType,
		Repo:     raw.Project.PathWithNamespace,
		Commit:   raw.CheckoutSha,
		Author:   raw.UserName,
	}

	if strings.HasPrefix(raw.Ref, "refs/heads/") {
		event.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	} else if strings.HasPrefix(raw.Ref, "refs/tags/") {
		event.Tag = strings.TrimPrefix(raw.Ref, "refs/tags/")
	}

	return event, nil
}
