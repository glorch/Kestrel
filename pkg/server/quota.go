package server

import (
	"fmt"
	"sync"
)

// TenantQuota defines resource limits allocated to a business unit or tenant.
type TenantQuota struct {
	TenantName    string `json:"tenant_name"`
	MaxConcurrent int    `json:"max_concurrent"`
	ActiveJobs    int    `json:"active_jobs"`
}

// QuotaManager tracks and limits concurrent job executions across tenants.
type QuotaManager struct {
	mu     sync.RWMutex
	quotas map[string]*TenantQuota
}

// NewQuotaManager creates a new QuotaManager.
func NewQuotaManager() *QuotaManager {
	return &QuotaManager{
		quotas: make(map[string]*TenantQuota),
	}
}

// SetQuota configures the maximum concurrent jobs permitted for a tenant.
func (qm *QuotaManager) SetQuota(tenant string, maxConcurrent int) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	q, exists := qm.quotas[tenant]
	if exists {
		q.MaxConcurrent = maxConcurrent
	} else {
		qm.quotas[tenant] = &TenantQuota{
			TenantName:    tenant,
			MaxConcurrent: maxConcurrent,
		}
	}
}

// TryAcquire attempts to increment a tenant's active job counter if within quota.
func (qm *QuotaManager) TryAcquire(tenant string) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if tenant == "" {
		return true, nil // Unassigned tenant has no quota constraints
	}

	q, ok := qm.quotas[tenant]
	if !ok {
		return true, nil // No limit set for tenant
	}

	if q.MaxConcurrent > 0 && q.ActiveJobs >= q.MaxConcurrent {
		return false, fmt.Errorf("tenant '%s' reached concurrency quota limit (%d/%d)", tenant, q.ActiveJobs, q.MaxConcurrent)
	}

	q.ActiveJobs++
	return true, nil
}

// Release decrements a tenant's active job counter upon job completion.
func (qm *QuotaManager) Release(tenant string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if tenant == "" {
		return
	}

	if q, ok := qm.quotas[tenant]; ok && q.ActiveJobs > 0 {
		q.ActiveJobs--
	}
}

// GetQuota returns current quota usage for a tenant.
func (qm *QuotaManager) GetQuota(tenant string) (TenantQuota, bool) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	q, ok := qm.quotas[tenant]
	if !ok {
		return TenantQuota{}, false
	}
	return *q, true
}

// ListQuotas returns a copy of all configured tenant quotas.
func (qm *QuotaManager) ListQuotas() map[string]TenantQuota {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	res := make(map[string]TenantQuota, len(qm.quotas))
	for k, v := range qm.quotas {
		res[k] = *v
	}
	return res
}
