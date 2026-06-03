package supervisormanager

import "sync"

// Detach is the DETACH-NOT-KILL shutdown path for ONE supervisor ref (PM focus
// #3). It closes the AttachClient connection but DOES NOT signal/kill the
// supervisor or claude — they keep running, owned by init, ready for a future
// daemon to re-attach (ProbeAgent → Reattachable).
//
// This is the contrast with the OLD direct-claude path (AgentController.Shutdown)
// which SIGKILLs sessions: that path owns claude as a child and must kill it. The
// supervisor path DETACHES — the daemon goes away, the supervisor+claude survive.
// s3b wires this as the control-loop shutdown; s3a only provides it.
func Detach(ref *SupervisorRef) {
	if ref == nil {
		return
	}
	if ref.Client != nil {
		_ = ref.Client.Close() // close the socket conn; NO signal to the processes
		ref.Client = nil
	}
}

// Manager tracks the supervisors + home locks a single daemon is managing, so
// DetachAll can tear the daemon's side down (close clients + release locks)
// WITHOUT killing any supervisor/claude. It is the minimal registry s3b will
// populate; s3a provides it but does not wire it into the runtime.
type Manager struct {
	mu    sync.Mutex
	refs  map[string]*SupervisorRef // agentID → ref
	locks map[string]func()         // agentID → home-lock release
}

// NewManager constructs an empty Manager.
func NewManager() *Manager {
	return &Manager{
		refs:  make(map[string]*SupervisorRef),
		locks: make(map[string]func()),
	}
}

// Track records a ref (and optionally its home-lock release) under its agent id.
// A prior ref for the same agent is detached first (no kill) to avoid leaking a
// connection. release may be nil.
func (m *Manager) Track(ref *SupervisorRef, release func()) {
	if ref == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.refs[ref.AgentID]; ok && old != ref {
		Detach(old)
	}
	m.refs[ref.AgentID] = ref
	if release != nil {
		if oldRel, ok := m.locks[ref.AgentID]; ok && oldRel != nil {
			oldRel()
		}
		m.locks[ref.AgentID] = release
	}
}

// Get returns the tracked ref for an agent (nil, false if none).
func (m *Manager) Get(agentID string) (*SupervisorRef, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.refs[agentID]
	return ref, ok
}

// DetachAll is the daemon-shutdown path: detach every tracked ref (close clients,
// NO kill) and release every home lock. The supervisors + claudes keep running.
func (m *Manager) DetachAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ref := range m.refs {
		Detach(ref)
		delete(m.refs, id)
	}
	for id, release := range m.locks {
		if release != nil {
			release()
		}
		delete(m.locks, id)
	}
}
