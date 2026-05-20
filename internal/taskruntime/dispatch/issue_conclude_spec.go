package dispatch

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// IssueConcludeTaskSpec describes a single Task to spawn from an Issue
// conclude action. `LocalID` is a batch-local identifier resolvable from
// other tasks' `DependsOn` lists (allows N-task spawn with batch-internal
// dependencies).
type IssueConcludeTaskSpec struct {
	LocalID           string               // batch-internal identifier
	Title             string
	Description       string
	Priority          task.Priority
	RequiresWorktree  bool
	DependsOnLocalIDs []string             // refs into batch
	DependsOnTaskIDs  []taskruntime.TaskID // refs into already-existing tasks
	EtaAt             *time.Time
	ParentTaskID      taskruntime.TaskID
}

// IssueConcludeSpec is the Discussion BC → TaskRuntime input (00-overview
// § 3.4). Phase 2 ships this as a stub (single contract); Phase 3 wires
// the actual Discussion caller.
type IssueConcludeSpec struct {
	IssueID        string
	ProjectID      string
	Resolution     string
	ClosingComment string
	Tasks          []IssueConcludeTaskSpec
	ActorID        string // user:hayang / supervisor:inv-x
}

// Validate enforces top-level invariants. Per-task validation + dep graph
// is handled inside IssueConcludeSpawn.Spawn.
func (s IssueConcludeSpec) Validate() error {
	if strings.TrimSpace(s.IssueID) == "" {
		return errors.New("issue_conclude_spec: issue_id required")
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return errors.New("issue_conclude_spec: project_id required")
	}
	if strings.TrimSpace(s.Resolution) == "" {
		return errors.New("issue_conclude_spec: resolution required")
	}
	if len(s.Tasks) == 0 {
		return errors.New("issue_conclude_spec: at least 1 task required")
	}
	if strings.TrimSpace(s.ActorID) == "" {
		return errors.New("issue_conclude_spec: actor_id required")
	}
	seen := make(map[string]struct{}, len(s.Tasks))
	for i, ts := range s.Tasks {
		if strings.TrimSpace(ts.LocalID) == "" {
			return fmt.Errorf("issue_conclude_spec: task[%d] local_id required", i)
		}
		if _, ok := seen[ts.LocalID]; ok {
			return fmt.Errorf("issue_conclude_spec: duplicate local_id %q", ts.LocalID)
		}
		seen[ts.LocalID] = struct{}{}
		if strings.TrimSpace(ts.Title) == "" {
			return fmt.Errorf("issue_conclude_spec: task[%s] title required", ts.LocalID)
		}
		if ts.Priority != "" && !ts.Priority.IsValid() {
			return fmt.Errorf("issue_conclude_spec: task[%s] invalid priority %q", ts.LocalID, ts.Priority)
		}
	}
	// Local-dep graph cycle detection via DFS (depth ≤ 32).
	if err := s.detectCycles(seen); err != nil {
		return err
	}
	return nil
}

func (s IssueConcludeSpec) detectCycles(known map[string]struct{}) error {
	graph := make(map[string][]string, len(s.Tasks))
	for _, ts := range s.Tasks {
		for _, dep := range ts.DependsOnLocalIDs {
			if _, ok := known[dep]; !ok {
				return fmt.Errorf("issue_conclude_spec: task[%s] depends on unknown local_id %q", ts.LocalID, dep)
			}
		}
		graph[ts.LocalID] = append([]string(nil), ts.DependsOnLocalIDs...)
	}
	const maxDepth = 32
	state := make(map[string]int) // 0 unvisited / 1 in-progress / 2 done
	var dfs func(string, int) error
	dfs = func(n string, depth int) error {
		if depth > maxDepth {
			return fmt.Errorf("issue_conclude_spec: dep graph depth > %d (likely cycle near %q)", maxDepth, n)
		}
		switch state[n] {
		case 1:
			return fmt.Errorf("issue_conclude_spec: dep cycle detected at %q", n)
		case 2:
			return nil
		}
		state[n] = 1
		for _, next := range graph[n] {
			if err := dfs(next, depth+1); err != nil {
				return err
			}
		}
		state[n] = 2
		return nil
	}
	for n := range graph {
		if err := dfs(n, 0); err != nil {
			return err
		}
	}
	return nil
}
