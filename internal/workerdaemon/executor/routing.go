package executor

// routing.go implements F4 of the agent-concurrent-execution design
// (docs/design/features/agent-concurrent-execution.md §8 / §11.1 step a): the
// consistency-routing layer that lets an agent's resident orchestrator (监工)
// decide which in-flight *problem* an incoming chat message / center event
// belongs to, so the same problem discussed across multiple chats merges into one
// execution thread instead of spawning a duplicate executor every time.
//
// The durable state is a single JSON file at <agent_root>/routing.json — a
// SIBLING of executors/, not a per-executor file (design §8). It records the
// problem → chat / issue / task / executor mapping:
//
//	{
//	  "problems": [
//	    {
//	      "problem_id": "...",
//	      "issue_ref": "issue-...",      // explicit binding — highest-priority key
//	      "task_refs": ["task-..."],     // explicit binding — see Route priority
//	      "chat_ids": ["channel-..."],   // which chats reference this problem
//	      "executor_ids": ["..."],       // executors spawned for this problem
//	      "created_at": "..."
//	    }
//	  ]
//	}
//
// Routing precedence is "explicit binding first, LLM semantic merge only as an
// auxiliary hint" (design §8): issue_ref ▸ task_ref ▸ chat_id ▸ semantic_hint.
// The center-issued issue/task refs are deterministic and authoritative; the
// semantic hint is the orchestrator's LLM guess and is consulted last.
//
// Like the rest of this package (F2 file protocol, F1 process model) routing.json
// is the orchestrator's DURABLE state: on crash recovery the orchestrator reloads
// it together with a Scan of executors/ to rebuild "which problem owns which
// executor" without losing or double-spawning work (design §12). The orchestrator
// is the single writer (design §3), so the store does a plain load-mutate-save;
// writes are atomic (temp+rename, shared with exchange.go) so a crash mid-write
// never leaves a torn table.
//
// This package owns ONLY the file contract and the pure routing decision. ID
// generation, LLM difficulty/semantic judgement, and actually spawning/feeding an
// executor belong to the orchestrator (consumes this layer); problem_id is
// supplied by the caller, never minted here.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// routingFileName is the routing-table leaf under <agent_root> (design §8). It is
// part of the on-disk contract the orchestrator relies on across restarts.
const routingFileName = "routing.json"

// MatchReason explains why a Signal routed to a problem (or to none). It is
// recorded on the Decision so the orchestrator can log/inspect WHY a message was
// merged — explicit binding vs auxiliary LLM hint — rather than treating routing
// as an opaque oracle.
type MatchReason string

const (
	// MatchIssueRef — matched on an explicit center issue binding (highest priority).
	MatchIssueRef MatchReason = "issue_ref"
	// MatchTaskRef — matched on an explicit center task binding.
	MatchTaskRef MatchReason = "task_ref"
	// MatchChatID — matched because the source chat is already mapped to a problem.
	MatchChatID MatchReason = "chat_id"
	// MatchSemantic — matched on the orchestrator's LLM semantic hint (auxiliary only).
	MatchSemantic MatchReason = "semantic_hint"
	// MatchNone — no existing problem matched; the caller should create a new one.
	MatchNone MatchReason = ""
)

// Signal is the routing input the orchestrator distils from an incoming chat
// message or a center task event (design §11.1 step a). All fields are optional;
// routing uses whichever explicit refs are present and falls back to the LLM
// semantic hint last.
type Signal struct {
	// ChatID is the source chat the message arrived on (e.g. "channel-...").
	ChatID string
	// IssueRef / TaskRef are the explicit center bindings, when known. These are
	// authoritative — a message carrying a known issue/task ref routes to that
	// problem regardless of which chat it came from.
	IssueRef string
	TaskRef  string
	// SemanticHint is an existing problem_id the orchestrator's LLM believes this
	// message is about. Consulted ONLY when no explicit ref matches (design §8:
	// "LLM 语义归并仅作辅助提示"). A hint that names no existing problem is ignored.
	SemanticHint string
}

// Decision is the routing verdict for a Signal. When IsNew is true no existing
// problem matched and the caller should register a fresh problem (Reason is
// MatchNone, ProblemID is empty). Otherwise ProblemID names the problem the
// message merges into and Reason records which key matched.
type Decision struct {
	ProblemID string
	Reason    MatchReason
	IsNew     bool
}

// Problem is one entry in routing.json: a logical topic (≈ issue) that may be
// referenced by several chats and served by several executors (design §2/§8).
type Problem struct {
	ProblemID   string    `json:"problem_id"`
	IssueRef    string    `json:"issue_ref,omitempty"`
	TaskRefs    []string  `json:"task_refs,omitempty"`
	ChatIDs     []string  `json:"chat_ids,omitempty"`
	ExecutorIDs []string  `json:"executor_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Validate enforces the invariants the orchestrator relies on. A problem_id is
// required and must be non-empty / null-byte-free (it is an identity key, never
// silently defaulted — conventions §16/§17). created_at must be set (the store
// stamps it on Register when zero, so a hand-written table without it is rejected
// rather than treated as the zero time).
func (p Problem) Validate() error {
	if err := validateProblemID(p.ProblemID); err != nil {
		return err
	}
	if p.CreatedAt.IsZero() {
		return fmt.Errorf("executor: problem %q created_at required", p.ProblemID)
	}
	return nil
}

// validateProblemID rejects an empty or null-byte-bearing id. Unlike an executor
// id, a problem_id is never joined into a filesystem path (it lives only inside
// routing.json), so path-separator/traversal checks do not apply — but it is still
// an identity key and must be a real value.
func validateProblemID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("executor: problem_id required")
	}
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("executor: problem_id %q contains null byte", id)
	}
	return nil
}

// RoutingTable is the in-memory form of routing.json. All routing logic and
// mutation lives here as pure methods over the slice; RoutingStore handles
// persistence. The Route decision never mutates.
type RoutingTable struct {
	Problems []Problem `json:"problems"`
}

// Validate checks every problem and that problem_ids are unique (a duplicate id
// would make routing ambiguous).
func (t *RoutingTable) Validate() error {
	seen := make(map[string]struct{}, len(t.Problems))
	for i := range t.Problems {
		p := t.Problems[i]
		if err := p.Validate(); err != nil {
			return err
		}
		if _, dup := seen[p.ProblemID]; dup {
			return fmt.Errorf("executor: duplicate problem_id %q in routing table", p.ProblemID)
		}
		seen[p.ProblemID] = struct{}{}
	}
	return nil
}

// Route resolves which existing problem a Signal belongs to, applying the design
// §8 precedence: explicit issue_ref ▸ explicit task_ref ▸ chat_id ▸ LLM
// semantic_hint. The first key that matches an existing problem wins; if none
// match, the verdict is IsNew (the caller registers a fresh problem). Route is
// pure — it reads the table and returns a verdict, never mutating.
func (t *RoutingTable) Route(sig Signal) Decision {
	// 1. Explicit issue binding — authoritative, regardless of source chat.
	if ref := strings.TrimSpace(sig.IssueRef); ref != "" {
		for i := range t.Problems {
			if t.Problems[i].IssueRef == ref {
				return Decision{ProblemID: t.Problems[i].ProblemID, Reason: MatchIssueRef}
			}
		}
	}
	// 2. Explicit task binding.
	if ref := strings.TrimSpace(sig.TaskRef); ref != "" {
		for i := range t.Problems {
			if containsString(t.Problems[i].TaskRefs, ref) {
				return Decision{ProblemID: t.Problems[i].ProblemID, Reason: MatchTaskRef}
			}
		}
	}
	// 3. Chat already mapped to a problem — continuity for an ongoing chat.
	if chat := strings.TrimSpace(sig.ChatID); chat != "" {
		for i := range t.Problems {
			if containsString(t.Problems[i].ChatIDs, chat) {
				return Decision{ProblemID: t.Problems[i].ProblemID, Reason: MatchChatID}
			}
		}
	}
	// 4. LLM semantic hint — auxiliary only; honoured iff it names an existing problem.
	if hint := strings.TrimSpace(sig.SemanticHint); hint != "" {
		if _, ok := t.find(hint); ok {
			return Decision{ProblemID: hint, Reason: MatchSemantic}
		}
	}
	// 5. No match → new problem.
	return Decision{Reason: MatchNone, IsNew: true}
}

// Find returns a copy of the problem with the given id and whether it exists.
func (t *RoutingTable) Find(problemID string) (Problem, bool) {
	if p, ok := t.find(problemID); ok {
		return *p, true
	}
	return Problem{}, false
}

// find returns a pointer into the slice for in-place mutation (internal).
func (t *RoutingTable) find(problemID string) (*Problem, bool) {
	for i := range t.Problems {
		if t.Problems[i].ProblemID == problemID {
			return &t.Problems[i], true
		}
	}
	return nil, false
}

// Add appends a new problem after validating it and ensuring its id is unique.
// The set-valued fields are de-duplicated so a caller-supplied record with
// repeats is normalised. Add is the in-memory counterpart of RoutingStore.Register.
func (t *RoutingTable) Add(p Problem) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if _, dup := t.find(p.ProblemID); dup {
		return fmt.Errorf("executor: problem_id %q already exists", p.ProblemID)
	}
	p.TaskRefs = dedupe(p.TaskRefs)
	p.ChatIDs = dedupe(p.ChatIDs)
	p.ExecutorIDs = dedupe(p.ExecutorIDs)
	t.Problems = append(t.Problems, p)
	return nil
}

// AttachChat records that chatID references problemID (set-union, idempotent).
func (t *RoutingTable) AttachChat(problemID, chatID string) error {
	return t.mutate(problemID, func(p *Problem) error {
		if strings.TrimSpace(chatID) == "" {
			return errors.New("executor: chat_id required")
		}
		p.ChatIDs = addToSet(p.ChatIDs, chatID)
		return nil
	})
}

// AttachExecutor records that executorID was spawned for problemID (set-union,
// idempotent). The executor id is validated so routing.json never records an id
// that could not name a real executor dir.
func (t *RoutingTable) AttachExecutor(problemID, executorID string) error {
	return t.mutate(problemID, func(p *Problem) error {
		if err := validateExecutorID(executorID); err != nil {
			return err
		}
		p.ExecutorIDs = addToSet(p.ExecutorIDs, executorID)
		return nil
	})
}

// AttachTaskRef records an explicit task binding onto problemID (set-union,
// idempotent), so a later message carrying that task ref routes here.
func (t *RoutingTable) AttachTaskRef(problemID, taskRef string) error {
	return t.mutate(problemID, func(p *Problem) error {
		if strings.TrimSpace(taskRef) == "" {
			return errors.New("executor: task_ref required")
		}
		p.TaskRefs = addToSet(p.TaskRefs, taskRef)
		return nil
	})
}

// SetIssueRef binds problemID to issueRef (the explicit, highest-priority key).
// A problem carries at most one issue_ref; rebinding to a DIFFERENT issue is
// refused rather than silently overwriting an authoritative binding. Re-setting
// the same ref is a no-op.
func (t *RoutingTable) SetIssueRef(problemID, issueRef string) error {
	return t.mutate(problemID, func(p *Problem) error {
		ref := strings.TrimSpace(issueRef)
		if ref == "" {
			return errors.New("executor: issue_ref required")
		}
		if p.IssueRef != "" && p.IssueRef != ref {
			return fmt.Errorf("executor: problem %q already bound to issue %q, refusing rebind to %q", problemID, p.IssueRef, ref)
		}
		p.IssueRef = ref
		return nil
	})
}

func (t *RoutingTable) mutate(problemID string, fn func(*Problem) error) error {
	p, ok := t.find(problemID)
	if !ok {
		return fmt.Errorf("executor: problem %q not found", problemID)
	}
	return fn(p)
}

// -----------------------------------------------------------------------------
// RoutingStore — persistence over <agent_root>/routing.json
// -----------------------------------------------------------------------------

// RoutingStore loads and saves the routing table at <agent_root>/routing.json.
// The orchestrator is the single writer (design §3), so each mutating helper does
// a plain load → mutate → atomic save; there is no cross-process locking because
// the protocol guarantees exactly one writer. The injected clock stamps
// CreatedAt so tests stay deterministic (conventions §14.x).
type RoutingStore struct {
	path string
	clk  clock.Clock
}

// RoutingPath is <agent_root>/routing.json — the routing table's on-disk home.
func (l *Layout) RoutingPath() string {
	return filepath.Join(l.agentRoot, routingFileName)
}

// NewRoutingStore anchors a store at agentRoot's routing.json. A nil clock
// defaults to the system clock (UTC). The file need not exist yet — Load treats a
// missing file as an empty table.
func NewRoutingStore(agentRoot string, clk clock.Clock) (*RoutingStore, error) {
	l, err := NewLayout(agentRoot)
	if err != nil {
		return nil, err
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &RoutingStore{path: l.RoutingPath(), clk: clk}, nil
}

// Load reads + validates routing.json. A missing file is NOT an error — it yields
// an empty table (the agent simply has no in-flight problems yet, design §12). A
// present-but-corrupt or invalid file IS an error, never silently zeroed
// (conventions §17), so a torn table surfaces instead of dropping live routing.
func (s *RoutingStore) Load() (*RoutingTable, error) {
	var t RoutingTable
	if err := readJSON(s.path, &t); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &RoutingTable{}, nil
		}
		return nil, fmt.Errorf("executor: read routing.json: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("executor: routing.json invalid: %w", err)
	}
	return &t, nil
}

// Save validates + atomically writes the table (temp+rename, shared with the F2
// protocol writers) so a crash mid-write never tears routing.json.
func (s *RoutingStore) Save(t *RoutingTable) error {
	if t == nil {
		return errors.New("executor: nil routing table")
	}
	if err := t.Validate(); err != nil {
		return err
	}
	return writeJSONAtomic(s.path, t)
}

// Route loads the table and resolves the Signal (read-only). It is the
// orchestrator's "which problem does this message belong to?" query (design
// §11.1 step a).
func (s *RoutingStore) Route(sig Signal) (Decision, error) {
	t, err := s.Load()
	if err != nil {
		return Decision{}, err
	}
	return t.Route(sig), nil
}

// Register persists a brand-new problem (the "否 → 新建 problem 上下文，记入
// routing.json" branch of design §11.1 step a). CreatedAt is stamped from the
// clock when zero so callers need not set it. The problem_id is caller-supplied;
// registering a duplicate id is an error.
func (s *RoutingStore) Register(p Problem) error {
	t, err := s.Load()
	if err != nil {
		return err
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = s.clk.Now().UTC()
	}
	if err := t.Add(p); err != nil {
		return err
	}
	return s.Save(t)
}

// Merge attaches the Signal's source chat (and any explicit task/issue ref it
// carries) plus the given executor ids onto an existing problem, then persists
// (the "是 → 归并" branch of design §11.1 step a). It is idempotent: re-merging
// the same chat/executor is a no-op. issueRef rebinding to a different issue is
// refused (see SetIssueRef). A problemID that does not exist is an error.
func (s *RoutingStore) Merge(problemID string, sig Signal, executorIDs ...string) error {
	t, err := s.Load()
	if err != nil {
		return err
	}
	if _, ok := t.find(problemID); !ok {
		return fmt.Errorf("executor: problem %q not found", problemID)
	}
	if chat := strings.TrimSpace(sig.ChatID); chat != "" {
		if err := t.AttachChat(problemID, chat); err != nil {
			return err
		}
	}
	if ref := strings.TrimSpace(sig.IssueRef); ref != "" {
		if err := t.SetIssueRef(problemID, ref); err != nil {
			return err
		}
	}
	if ref := strings.TrimSpace(sig.TaskRef); ref != "" {
		if err := t.AttachTaskRef(problemID, ref); err != nil {
			return err
		}
	}
	for _, eid := range executorIDs {
		if strings.TrimSpace(eid) == "" {
			continue
		}
		if err := t.AttachExecutor(problemID, eid); err != nil {
			return err
		}
	}
	return s.Save(t)
}

// -----------------------------------------------------------------------------
// small set helpers
// -----------------------------------------------------------------------------

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// addToSet appends v unless already present (preserves insertion order).
func addToSet(xs []string, v string) []string {
	if containsString(xs, v) {
		return xs
	}
	return append(xs, v)
}

// dedupe returns xs with duplicates removed, preserving first-seen order. A nil
// or all-unique slice is returned as-is (nil stays nil so omitempty drops it).
func dedupe(xs []string) []string {
	if len(xs) == 0 {
		return xs
	}
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
