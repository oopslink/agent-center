package identity

import "sync"

// OrganizationLockManager provides in-process per-organization mutexes to
// guard against concurrent race conditions on owner-count invariants (DS-2 in
// v2.6-design § 4.8.2).
//
// Single-machine SQLite single-writer + Go memory lock is sufficient for v2.6.
type OrganizationLockManager struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewOrganizationLockManager creates a new lock manager.
func NewOrganizationLockManager() *OrganizationLockManager {
	return &OrganizationLockManager{locks: make(map[string]*sync.Mutex)}
}

// WithLock acquires the per-organization lock for the duration of fn.
func (m *OrganizationLockManager) WithLock(organizationID string, fn func() error) error {
	m.mu.Lock()
	if m.locks[organizationID] == nil {
		m.locks[organizationID] = &sync.Mutex{}
	}
	lock := m.locks[organizationID]
	m.mu.Unlock()

	lock.Lock()
	defer lock.Unlock()
	return fn()
}
