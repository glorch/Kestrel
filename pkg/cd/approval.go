package cd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ApprovalStatus represents the current state of an approval gate.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "PENDING"
	ApprovalApproved ApprovalStatus = "APPROVED"
	ApprovalRejected ApprovalStatus = "REJECTED"
	ApprovalExpired  ApprovalStatus = "EXPIRED"
)

var (
	ErrGateNotFound   = errors.New("approval gate not found")
	ErrAlreadyDecided = errors.New("approval gate has already been decided")
	ErrUnauthorized   = errors.New("user is not authorized to approve this gate")
)

// GateRequest represents an approval gate blocking deployment to an environment.
type GateRequest struct {
	ID          string         `json:"id"`
	RunID       string         `json:"run_id"`
	JobID       string         `json:"job_id"`
	Environment string         `json:"environment"`
	Approvers   []string       `json:"approvers,omitempty"` // If empty, any operator can approve
	Status      ApprovalStatus `json:"status"`
	Approver    string         `json:"approver,omitempty"`
	Comment     string         `json:"comment,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   time.Time      `json:"expires_at"`
	DecidedAt   *time.Time     `json:"decided_at,omitempty"`

	notifyCh chan ApprovalStatus `json:"-"`
}

// GateManager manages lifecycle, timeouts, and decision signaling for approval gates.
type GateManager struct {
	mu    sync.RWMutex
	gates map[string]*GateRequest
}

// NewGateManager creates a new GateManager.
func NewGateManager() *GateManager {
	return &GateManager{
		gates: make(map[string]*GateRequest),
	}
}

// RequestApproval registers an approval gate and sets its expiration timeout.
func (gm *GateManager) RequestApproval(runID, jobID, env string, approvers []string, timeout time.Duration) *GateRequest {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if timeout <= 0 {
		timeout = 24 * time.Hour
	}

	gateID := fmt.Sprintf("gate-%s-%s-%d", runID, jobID, time.Now().UnixNano()%10000)
	now := time.Now()

	req := &GateRequest{
		ID:          gateID,
		RunID:       runID,
		JobID:       jobID,
		Environment: env,
		Approvers:   approvers,
		Status:      ApprovalPending,
		CreatedAt:   now,
		ExpiresAt:   now.Add(timeout),
		notifyCh:    make(chan ApprovalStatus, 1),
	}

	gm.gates[gateID] = req
	return req
}

// Approve grants approval for the specified gate.
func (gm *GateManager) Approve(gateID, approver, comment string) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	req, ok := gm.gates[gateID]
	if !ok {
		return ErrGateNotFound
	}

	if req.Status != ApprovalPending {
		return ErrAlreadyDecided
	}

	if len(req.Approvers) > 0 && !isAuthorized(approver, req.Approvers) {
		return ErrUnauthorized
	}

	now := time.Now()
	req.Status = ApprovalApproved
	req.Approver = approver
	req.Comment = comment
	req.DecidedAt = &now

	select {
	case req.notifyCh <- ApprovalApproved:
	default:
	}

	return nil
}

// Reject declines the approval gate.
func (gm *GateManager) Reject(gateID, approver, comment string) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	req, ok := gm.gates[gateID]
	if !ok {
		return ErrGateNotFound
	}

	if req.Status != ApprovalPending {
		return ErrAlreadyDecided
	}

	if len(req.Approvers) > 0 && !isAuthorized(approver, req.Approvers) {
		return ErrUnauthorized
	}

	now := time.Now()
	req.Status = ApprovalRejected
	req.Approver = approver
	req.Comment = comment
	req.DecidedAt = &now

	select {
	case req.notifyCh <- ApprovalRejected:
	default:
	}

	return nil
}

// WaitForDecision blocks until the gate is approved, rejected, expired, or context canceled.
func (gm *GateManager) WaitForDecision(ctx context.Context, gateID string) (ApprovalStatus, error) {
	gm.mu.RLock()
	req, ok := gm.gates[gateID]
	gm.mu.RUnlock()

	if !ok {
		return "", ErrGateNotFound
	}

	if req.Status != ApprovalPending {
		return req.Status, nil
	}

	timer := time.NewTimer(time.Until(req.ExpiresAt))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		gm.mu.Lock()
		if req.Status == ApprovalPending {
			req.Status = ApprovalExpired
		}
		gm.mu.Unlock()
		return ApprovalExpired, nil
	case status := <-req.notifyCh:
		return status, nil
	}
}

// ListPending returns all gates currently awaiting human approval.
func (gm *GateManager) ListPending() []*GateRequest {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	var list []*GateRequest
	for _, req := range gm.gates {
		if req.Status == ApprovalPending && time.Now().Before(req.ExpiresAt) {
			list = append(list, req)
		}
	}
	return list
}

// Get returns the gate by ID.
func (gm *GateManager) Get(gateID string) (*GateRequest, error) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	req, ok := gm.gates[gateID]
	if !ok {
		return nil, ErrGateNotFound
	}
	return req, nil
}

func isAuthorized(user string, allowed []string) bool {
	for _, a := range allowed {
		if a == user || a == "*" {
			return true
		}
	}
	return false
}
