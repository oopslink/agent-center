package agentlauncher

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// FilePIDStore is the production PIDStore: a single JSON file (agentID→pid) under the
// worker's runtime dir. It survives the worker process, so a worker restart reads back
// the pids of agent processes that outlived it (T860 gap5). Writes are whole-file and
// mutex-guarded; a corrupt/absent file loads as empty (the worst case is a respawn, not
// a crash).
type FilePIDStore struct {
	path string
	mu   sync.Mutex
}

// NewFilePIDStore returns a store backed by `path`, creating the parent dir.
func NewFilePIDStore(path string) (*FilePIDStore, error) {
	if path == "" {
		return nil, errors.New("agentlauncher: pid store path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &FilePIDStore{path: path}, nil
}

var _ PIDStore = (*FilePIDStore)(nil)

// loadLocked reads the map (caller holds mu). A missing/corrupt file → empty map.
func (s *FilePIDStore) loadLocked() map[string]int {
	m := make(map[string]int)
	b, err := os.ReadFile(s.path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m) // corrupt → empty (respawn, don't crash)
	return m
}

func (s *FilePIDStore) writeLocked(m map[string]int) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	// Atomic-ish: write a temp then rename so a crash mid-write never leaves a partial.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Record persists agentID→pid.
func (s *FilePIDStore) Record(agentID string, pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.loadLocked()
	m[agentID] = pid
	return s.writeLocked(m)
}

// Remove drops the agent's recorded pid.
func (s *FilePIDStore) Remove(agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.loadLocked()
	if _, ok := m[agentID]; !ok {
		return nil
	}
	delete(m, agentID)
	return s.writeLocked(m)
}

// Load returns the full agentID→pid map.
func (s *FilePIDStore) Load() (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(), nil
}
