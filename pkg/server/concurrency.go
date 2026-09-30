package server

import (
	"context"
	"fmt"
	"sync"
)

// ActiveGroupLock records which run or job currently holds the exclusive lock for a concurrency group.
type ActiveGroupLock struct {
	Group            string
	HolderID         string
	CancelInProgress bool
	CancelFunc       context.CancelFunc
}

// ConcurrencyManager manages mutually exclusive execution locks and in-flight cancellation.
type ConcurrencyManager struct {
	mu     sync.Mutex
	locks  map[string]*ActiveGroupLock // group -> current holder
	queued map[string][]string         // group -> queue of waiting IDs
}

// NewConcurrencyManager creates a new concurrency manager.
func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{
		locks:  make(map[string]*ActiveGroupLock),
		queued: make(map[string][]string),
	}
}

// Acquire requests exclusive execution for the given group.
// Returns:
//   - acquired: true if lock was granted immediately
//   - cancelledHolderID: non-empty if an older in-progress execution was cancelled to make room
//   - err: error if rejected
func (cm *ConcurrencyManager) Acquire(
	group string,
	holderID string,
	cancelInProgress bool,
	cancelFn context.CancelFunc,
) (acquired bool, cancelledHolderID string, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if group == "" {
		return true, "", nil // No concurrency constraints
	}

	current, exists := cm.locks[group]
	if !exists {
		// Group is free, acquire immediately
		cm.locks[group] = &ActiveGroupLock{
			Group:            group,
			HolderID:         holderID,
			CancelInProgress: cancelInProgress,
			CancelFunc:       cancelFn,
		}
		return true, "", nil
	}

	// Group already held by another execution
	if cancelInProgress {
		// Cancel current in-flight execution and take over
		cancelledHolderID = current.HolderID
		if current.CancelFunc != nil {
			current.CancelFunc()
		}

		cm.locks[group] = &ActiveGroupLock{
			Group:            group,
			HolderID:         holderID,
			CancelInProgress: cancelInProgress,
			CancelFunc:       cancelFn,
		}
		return true, cancelledHolderID, nil
	}

	// Otherwise, add to wait queue
	cm.queued[group] = append(cm.queued[group], holderID)
	return false, "", fmt.Errorf("concurrency group '%s' is locked by '%s'", group, current.HolderID)
}

// Release frees the lock for the given group and promotes the next waiting candidate if any.
func (cm *ConcurrencyManager) Release(group string, holderID string) (nextHolderID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	current, exists := cm.locks[group]
	if !exists || current.HolderID != holderID {
		return ""
	}

	queue := cm.queued[group]
	if len(queue) > 0 {
		next := queue[0]
		cm.queued[group] = queue[1:]
		cm.locks[group] = &ActiveGroupLock{
			Group:    group,
			HolderID: next,
		}
		return next
	}

	delete(cm.locks, group)
	return ""
}

// IsLocked checks whether a concurrency group is currently active.
func (cm *ConcurrencyManager) IsLocked(group string) (bool, string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if current, exists := cm.locks[group]; exists {
		return true, current.HolderID
	}
	return false, ""
}
