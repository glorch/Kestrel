package cd

import (
	"context"
	"testing"
	"time"
)

func TestApprovalFlow(t *testing.T) {
	gm := NewGateManager()
	gate := gm.RequestApproval("run-1", "deploy-prod", "production", []string{"alice", "bob"}, 2*time.Second)

	pending := gm.ListPending()
	if len(pending) != 1 || pending[0].ID != gate.ID {
		t.Fatalf("expected 1 pending gate, got %d", len(pending))
	}

	// Unauthorized attempt
	err := gm.Approve(gate.ID, "charlie", "trying to approve")
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized for charlie, got %v", err)
	}

	// Authorized approval in goroutine
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = gm.Approve(gate.ID, "alice", "lgtm to release")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := gm.WaitForDecision(ctx, gate.ID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if status != ApprovalApproved {
		t.Errorf("expected status APPROVED, got %s", status)
	}
}

func TestRejectionFlow(t *testing.T) {
	gm := NewGateManager()
	gate := gm.RequestApproval("run-2", "deploy-prod", "production", nil, 2*time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = gm.Reject(gate.ID, "admin", "found blocking bug")
	}()

	status, err := gm.WaitForDecision(context.Background(), gate.ID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if status != ApprovalRejected {
		t.Errorf("expected status REJECTED, got %s", status)
	}
}

func TestExpirationFlow(t *testing.T) {
	gm := NewGateManager()
	gate := gm.RequestApproval("run-3", "deploy-prod", "production", nil, 50*time.Millisecond)

	status, err := gm.WaitForDecision(context.Background(), gate.ID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if status != ApprovalExpired {
		t.Errorf("expected status EXPIRED, got %s", status)
	}
}
