package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// =============================================================================
// Direct test for ReplaceCapabilities (previously 0% — only exercised via
// WorkerConfigService.SetCapabilityEnabled).
// =============================================================================

func TestWorkerRepo_ReplaceCapabilities_Verbatim(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	if err := repo.Save(context.Background(), newWorker(t, "W-1")); err != nil {
		t.Fatal(err)
	}
	// Replace with exact list (incoming Enabled honored, not merged).
	caps := []workforce.Capability{
		{AgentCLI: "codex", Detected: false, Enabled: false},
		{AgentCLI: "claude-code", Detected: true, Enabled: false},
	}
	if err := repo.ReplaceCapabilities(context.Background(), "W-1", caps, 1); err != nil {
		t.Fatalf("ReplaceCapabilities: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
	list := got.CapabilityList()
	if len(list) != 2 {
		t.Fatalf("expected 2 caps, got %d", len(list))
	}
	for _, c := range list {
		if c.Enabled {
			t.Fatalf("expected all Enabled=false (verbatim), got %+v", c)
		}
	}
}

func TestWorkerRepo_ReplaceCapabilities_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	err := repo.ReplaceCapabilities(context.Background(), "W-1",
		[]workforce.Capability{{AgentCLI: "x", Detected: true, Enabled: true}}, 99)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_ReplaceCapabilities_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.ReplaceCapabilities(context.Background(), "W-NOPE",
		[]workforce.Capability{{AgentCLI: "x", Detected: true, Enabled: true}}, 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// =============================================================================
// AgentInstanceRepo.UpdateConfig (previously 0%; only Mgmt service exercised it).
// =============================================================================

func TestAgentInstanceRepo_UpdateConfig_DirectPath(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	if err := repo.Save(context.Background(), newAI(t, "01HA", "ai-1", "W-1")); err != nil {
		t.Fatal(err)
	}
	newCap := 5
	if err := repo.UpdateConfig(context.Background(), "01HA", `{"k":"v"}`, &newCap, 1); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "01HA")
	if got.Config() != `{"k":"v"}` {
		t.Fatalf("config: %s", got.Config())
	}
	if got.MaxConcurrent() == nil || *got.MaxConcurrent() != 5 {
		t.Fatalf("max_concurrent: %v", got.MaxConcurrent())
	}
}

func TestAgentInstanceRepo_UpdateConfig_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "ai-1", "W-1"))
	err := repo.UpdateConfig(context.Background(), "01HA", `{}`, nil, 99)
	if !errors.Is(err, workforce.ErrAgentInstanceVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestAgentInstanceRepo_UpdateConfig_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	err := repo.UpdateConfig(context.Background(), "01H-MISSING", `{}`, nil, 1)
	if !errors.Is(err, workforce.ErrAgentInstanceNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// =============================================================================
// ExecutorFromCtx error paths: closed DB / tx in cancelled ctx.
//
// We close the DB explicitly before calling repo methods to force the
// underlying *sql.DB to return an error, exercising the early-exit error
// branch in each repo method.
// =============================================================================

func TestWorkerRepo_ExecutorFromCtx_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	// Close the DB; any subsequent operation should error.
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "W-1"); err == nil {
		t.Fatal("FindByID after Close should error")
	}
	if _, err := repo.FindByStatus(context.Background(), workforce.WorkerOnline); err == nil {
		t.Fatal("FindByStatus after Close should error")
	}
	if _, err := repo.FindAll(context.Background()); err == nil {
		t.Fatal("FindAll after Close should error")
	}
	if err := repo.Save(context.Background(), newWorker(t, "W-X")); err == nil {
		t.Fatal("Save after Close should error")
	}
	if err := repo.UpdateStatus(context.Background(), "W-X",
		workforce.WorkerOffline, workforce.WorkerOnline, 1); err == nil {
		t.Fatal("UpdateStatus after Close should error")
	}
	if err := repo.UpdateLastHeartbeatAt(context.Background(), "W-X", time.Now(), 0); err == nil {
		t.Fatal("UpdateLastHeartbeatAt after Close should error")
	}
	if err := repo.UpdateConfig(context.Background(), "W-X",
		workforce.WorkerConfigFields{Concurrency: &workforce.WorkerConcurrency{PerAgentType: 1}}, 1); err == nil {
		t.Fatal("UpdateConfig after Close should error")
	}
	if err := repo.UpdateCapabilities(context.Background(), "W-X",
		[]workforce.Capability{{AgentCLI: "x", Detected: true}}, 1); err == nil {
		t.Fatal("UpdateCapabilities after Close should error")
	}
	if err := repo.ReplaceCapabilities(context.Background(), "W-X",
		[]workforce.Capability{{AgentCLI: "x", Detected: true, Enabled: true}}, 1); err == nil {
		t.Fatal("ReplaceCapabilities after Close should error")
	}
}

func TestBootstrapTokenRepo_ExecutorFromCtx_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "01HID"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindByValueHash(context.Background(), "h"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindByWorkerID(context.Background(), "W-1"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindActiveByWorkerForUpdate(context.Background(), "W-1"); err == nil {
		t.Fatal()
	}
	if err := repo.Save(context.Background(), newActiveToken(t, "01HID", "W-1")); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateStatus(context.Background(),
		newActiveToken(t, "01HID", "W-1"), workforce.BootstrapTokenActive); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindExpired(context.Background(), time.Now()); err == nil {
		t.Fatal()
	}
}

func TestAgentInstanceRepo_ExecutorFromCtx_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "01HA"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindByName(context.Background(), "n"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindAll(context.Background(), workforce.AgentInstanceFilter{}); err == nil {
		t.Fatal()
	}
	if err := repo.Save(context.Background(), newAI(t, "01HA", "n", "W-1")); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateState(context.Background(), "01HA",
		workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateConfig(context.Background(), "01HA", "{}", nil, 1); err == nil {
		t.Fatal()
	}
	if err := repo.Archive(context.Background(), "01HA", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "msg", 1); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// scan helper error paths: hand-craft a row with malformed timestamps to
// exercise the time.Parse error branches.
// =============================================================================

func TestWorkerRepo_Scan_BadTimestamp(t *testing.T) {
	db := openTestDB(t)
	// Manually insert a row with a malformed enrolled_at timestamp.
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BAD', 'offline', '{}', '{}', '[]', 0, 'not-a-time',
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BAD"); err == nil {
		t.Fatal("expected parse error on bad enrolled_at")
	}
}

func TestBootstrapTokenRepo_Scan_BadTimestamp(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO bootstrap_tokens
		(id, worker_id, value_hash, status, created_at, expires_at, created_by)
		VALUES ('01H-BAD', 'W-1', 'h', 'active', 'not-a-time',
		        '2026-05-22T00:30:00Z', 'user:x')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewBootstrapTokenRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BAD"); err == nil {
		t.Fatal("expected parse error on bad created_at")
	}
}

func TestAgentInstanceRepo_Scan_BadTimestamp(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO agent_instances
		(id, name, agent_cli, worker_id, config, state, is_builtin, created_at, version)
		VALUES ('01HA-BAD', 'bad', 'claude-code', 'W-1', '{}', 'idle', 0,
		        'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewAgentInstanceRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HA-BAD"); err == nil {
		t.Fatal("expected parse error on bad created_at")
	}
}

// scanWorker has a NullTime parse error branch for last_heartbeat_at;
// inject malformed nullable timestamp.
func TestWorkerRepo_Scan_BadNullableTime(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 last_heartbeat_at, working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-HB', 'offline', '{}', '{}', '[]', 'not-a-time', 0,
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-HB"); err == nil {
		t.Fatal("expected parse error on bad last_heartbeat_at")
	}
}

// =============================================================================
// Bad JSON paths in scan: malformed JSON column triggers Unmarshal error.
// =============================================================================

func TestWorkerRepo_Scan_BadCapabilitiesJSON(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BJ', 'offline', '{}', '{}', '{not valid json',
		        0, '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BJ"); err == nil {
		t.Fatal("expected unmarshal error on bad capabilities_json")
	}
}

func TestWorkerRepo_Scan_BadConcurrencyJSON(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BC', 'offline', '{nope', '{}', '[]',
		        0, '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BC"); err == nil {
		t.Fatal("expected unmarshal error on bad concurrency_json")
	}
}

func TestWorkerRepo_Scan_BadDiscoveryJSON(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BD', 'offline', '{}', '{bad', '[]',
		        0, '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BD"); err == nil {
		t.Fatal("expected unmarshal error on bad discovery_json")
	}
}

// More scan-error tests for the remaining timestamp fields.
func TestWorkerRepo_Scan_BadCreatedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BCR', 'offline', '{}', '{}', '[]', 0,
		        '2026-05-22T00:00:00Z', 'not-a-time', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BCR"); err == nil {
		t.Fatal("expected created_at parse error")
	}
}

func TestWorkerRepo_Scan_BadUpdatedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BU', 'offline', '{}', '{}', '[]', 0,
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BU"); err == nil {
		t.Fatal("expected updated_at parse error")
	}
}

func TestWorkerRepo_Scan_BadOnlineOffline(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 online_at, working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BO', 'online', '{}', '{}', '[]', 'not-a-time', 0,
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkerRepo(db)
	if _, err := repo.FindByID(context.Background(), "W-BO"); err == nil {
		t.Fatal("expected online_at parse error")
	}

	_, err = db.ExecContext(context.Background(), `INSERT INTO workers
		(id, status, concurrency_json, discovery_json, capabilities_json,
		 offline_at, working_seconds, enrolled_at, created_at, updated_at, version)
		VALUES ('W-BOF', 'offline', '{}', '{}', '[]', 'not-a-time', 0,
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(context.Background(), "W-BOF"); err == nil {
		t.Fatal("expected offline_at parse error")
	}
}

func TestBootstrapTokenRepo_Scan_BadExpiresAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO bootstrap_tokens
		(id, worker_id, value_hash, status, created_at, expires_at, created_by)
		VALUES ('01H-BE', 'W-1', 'h', 'active', '2026-05-22T00:00:00Z',
		        'not-a-time', 'user:x')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewBootstrapTokenRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BE"); err == nil {
		t.Fatal("expected expires_at parse error")
	}
}

func TestBootstrapTokenRepo_Scan_BadUsedOrRevokedAt(t *testing.T) {
	db := openTestDB(t)
	// used_at bad
	_, err := db.ExecContext(context.Background(), `INSERT INTO bootstrap_tokens
		(id, worker_id, value_hash, status, created_at, expires_at, used_at, created_by)
		VALUES ('01H-BUS', 'W-1', 'h1', 'used', '2026-05-22T00:00:00Z',
		        '2026-05-22T00:30:00Z', 'not-a-time', 'user:x')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewBootstrapTokenRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BUS"); err == nil {
		t.Fatal("expected used_at parse error")
	}
}

func TestAgentInstanceRepo_Scan_BadArchivedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO agent_instances
		(id, name, agent_cli, worker_id, config, state, is_builtin, created_at,
		 archived_at, version)
		VALUES ('01HA-BA', 'bad', 'claude-code', 'W-1', '{}', 'archived', 0,
		        '2026-05-22T00:00:00Z', 'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewAgentInstanceRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HA-BA"); err == nil {
		t.Fatal("expected archived_at parse error")
	}
}

// =============================================================================
// containsAny / indexOf coverage (private helpers in bootstrap_token_repo).
// =============================================================================

func TestBootstrapToken_ContainsAny_EmptyAndMiss(t *testing.T) {
	if containsAny("haystack") {
		t.Fatal("no needles should return false")
	}
	if containsAny("", "needle") {
		t.Fatal("empty haystack should return false")
	}
	if containsAny("haystack", "") {
		t.Fatal("empty needle should return false")
	}
	if !containsAny("hay needle stack", "needle") {
		t.Fatal("present needle should return true")
	}
}

// =============================================================================
// Save_DuplicateValueHash path branches (already covered for value_hash;
// also need the bare PK conflict — distinct id but with constraint name not
// matching value_hash / worker_id suffix → ErrBootstrapTokenAlreadyExists).
// =============================================================================

func TestBootstrapTokenRepo_Save_DuplicatePK(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	if err := repo.Save(context.Background(), newActiveToken(t, "01HID-X", "W-1")); err != nil {
		t.Fatal(err)
	}
	// Same id; different worker / value_hash → PK conflict.
	other := newActiveTokenWithHash(t, "01HID-X", "W-2", workforce.HashTokenValue("o"))
	err := repo.Save(context.Background(), other)
	if !errors.Is(err, workforce.ErrBootstrapTokenAlreadyExists) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

// =============================================================================
// AgentInstanceRepo.Archive variants — version conflict path.
// =============================================================================

func TestAgentInstanceRepo_Archive_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "ai-1", "W-1"))
	err := repo.Archive(context.Background(), "01HA", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "msg", 99)
	if !errors.Is(err, workforce.ErrAgentInstanceVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestAgentInstanceRepo_Archive_AlreadyArchived(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "ai-1", "W-1"))
	if err := repo.Archive(context.Background(), "01HA", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "first", 1); err != nil {
		t.Fatal(err)
	}
	// Second archive on already-archived row.
	err := repo.Archive(context.Background(), "01HA", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "second", 2)
	if !errors.Is(err, workforce.ErrAgentInstanceArchived) {
		t.Fatalf("expected archived, got %v", err)
	}
}

func TestAgentInstanceRepo_Archive_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	err := repo.Archive(context.Background(), "01H-NEVER", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "msg", 1)
	if !errors.Is(err, workforce.ErrAgentInstanceNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// =============================================================================
// AgentInstanceRepo.UpdateState — not found path
// =============================================================================

func TestAgentInstanceRepo_UpdateState_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	err := repo.UpdateState(context.Background(), "01H-NEVER",
		workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1)
	if !errors.Is(err, workforce.ErrAgentInstanceNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// FindAll with State filter (previously covered worker/builtin filters only).
func TestAgentInstanceRepo_FindAll_StateFilter(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1"))
	_ = repo.Save(context.Background(), newAI(t, "01HB", "a2", "W-2"))
	_ = repo.UpdateState(context.Background(), "01HA",
		workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1)
	active := workforce.AgentInstanceActive
	got, _ := repo.FindAll(context.Background(), workforce.AgentInstanceFilter{State: &active})
	if len(got) != 1 || got[0].Name() != "a1" {
		t.Fatalf("active filter: %v", got)
	}
}

// Ensure RehydrateBootstrapToken returns error on invalid status enum
// (covered indirectly via scan; the unit test of Rehydrate is also needed).
func TestRehydrateBootstrapToken_InvalidStatus(t *testing.T) {
	_, err := workforce.RehydrateBootstrapToken(workforce.RehydrateBootstrapTokenInput{
		ID: "01H", WorkerID: "W-1", ValueHash: "h",
		Status: "bogus", CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Minute), CreatedBy: "u",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
}

// =============================================================================
// nullWorkerID / nullInt / nullTimePtr edge cases used by AI repo.
// =============================================================================

func TestNullHelpers(t *testing.T) {
	if nullWorkerID(nil) != nil {
		t.Fatal()
	}
	wid := workforce.WorkerID("W-1")
	if nullWorkerID(&wid) != "W-1" {
		t.Fatal()
	}
	if nullInt(nil) != nil {
		t.Fatal()
	}
	v := 7
	if nullInt(&v) != 7 {
		t.Fatal()
	}
}

// =============================================================================
// Invalid-input early-exit branches for state-machine repo methods.
// =============================================================================

func TestAgentInstanceRepo_UpdateState_InvalidStateEnum(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	err := repo.UpdateState(context.Background(), "01HA",
		workforce.AgentInstanceState("bogus"),
		workforce.AgentInstanceActive, 1)
	if err == nil {
		t.Fatal("expected invalid state error")
	}
	err = repo.UpdateState(context.Background(), "01HA",
		workforce.AgentInstanceIdle,
		workforce.AgentInstanceState("bogus"), 1)
	if err == nil {
		t.Fatal("expected invalid state error")
	}
}

// AgentInstanceRepo.Archive — invalid (built-in) at runtime check path
// (currently only AR-level Archive method tests this; repo Archive sql guard
// returns ErrAgentInstanceIsBuiltin from the diagnose branch).
func TestAgentInstanceRepo_Archive_FromNonIdleNonBuiltin(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	// Put agent into sleeping state directly (bypass AR for test).
	_ = repo.Save(context.Background(), newAI(t, "01HA", "ai", "W-1"))
	_ = repo.UpdateState(context.Background(), "01HA",
		workforce.AgentInstanceIdle, workforce.AgentInstanceSleeping, 1)
	err := repo.Archive(context.Background(), "01HA", time.Now(),
		workforce.AgentInstanceArchivedReasonManual, "msg", 2)
	if err == nil {
		t.Fatal("expected archive rejection from sleeping")
	}
}

// AgentInstanceRepo.Save — built-in row with worker_id should error at AR
// constructor level, not reach repo; here we test the CHECK constraint at DB.
// The AR enforces this, but verifying the DB enforces it too.
func TestAgentInstanceRepo_Save_BuiltinWithWorkerIDViaRaw(t *testing.T) {
	db := openTestDB(t)
	// Manually try to insert an invalid built-in row (worker_id NOT NULL but
	// is_builtin = 1). The CHECK constraint should reject.
	_, err := db.ExecContext(context.Background(), `INSERT INTO agent_instances
		(id, name, agent_cli, worker_id, config, state, is_builtin, created_at, version)
		VALUES ('01HBAD', 'bad', 'claude-code', 'W-1', '{}', 'idle', 1, '2026-05-22T00:00:00Z', 1)`)
	if err == nil {
		t.Fatal("expected CHECK constraint violation")
	}
}

// Save with PK conflict (no name collision) → ErrAgentInstanceAlreadyExists.
func TestAgentInstanceRepo_Save_PKConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "first", "W-1"))
	err := repo.Save(context.Background(), newAI(t, "01HA", "second", "W-2"))
	if !errors.Is(err, workforce.ErrAgentInstanceAlreadyExists) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

// persistence import keeps build happy when refactoring; keep at file end.
var _ = persistence.RunInTx
