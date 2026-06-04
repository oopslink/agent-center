package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// v2.8 #255: unit coverage for the test-instance helper's pure logic
// (namespace/id allocation, port probe, layout derivation, slug validation =
// E1 path-traversal guard, worker-config discovery, namespace-confined
// teardown, self-documenting access-pack hints). The full 1-center+N-worker
// deployed smoke (A/B/C/D/E/F) is Tester's lane.

func TestAllocateTestInstanceID_255_Sequential(t *testing.T) {
	root := t.TempDir()
	for want := 1; want <= 3; want++ {
		id, err := allocateTestInstanceID(root)
		if err != nil {
			t.Fatalf("alloc %d: %v", want, err)
		}
		if id != "t"+strconv.Itoa(want) {
			t.Fatalf("alloc %d = %q, want t%d", want, id, want)
		}
	}
	// An out-of-band existing id is skipped.
	if err := os.Mkdir(filepath.Join(root, "t4"), 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := allocateTestInstanceID(root)
	if err != nil {
		t.Fatal(err)
	}
	if id != "t5" {
		t.Fatalf("alloc after pre-created t4 = %q, want t5", id)
	}
}

func TestAllocateTestInstanceID_255_ConcurrentRaceSafe(t *testing.T) {
	root := t.TempDir()
	const n = 50
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = allocateTestInstanceID(root)
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if seen[ids[i]] {
			t.Fatalf("duplicate id %q — race not safe", ids[i])
		}
		seen[ids[i]] = true
	}
	// Exactly t1..t50, no gaps/collisions (atomic mkdir claim).
	for k := 1; k <= n; k++ {
		if !seen["t"+strconv.Itoa(k)] {
			t.Fatalf("missing id t%d in concurrent allocation", k)
		}
	}
}

func TestAllocateThreeFreePorts_255_DistinctNoAirPlay(t *testing.T) {
	web, server, admin, err := allocateThreeFreePorts()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int{web, server, admin} {
		if p == 7000 {
			t.Fatalf("allocated the :7000 AirPlay port (#161)")
		}
		if p <= 0 {
			t.Fatalf("invalid port %d", p)
		}
	}
	if web == server || web == admin || server == admin {
		t.Fatalf("ports not distinct: web=%d server=%d admin=%d", web, server, admin)
	}
}

func TestValidInstanceName_255_E1RejectsTraversal(t *testing.T) {
	// E1: --id must be a safe slug — no path traversal / absolute / separators.
	for _, bad := range []string{"..", "../foo", "/abs", "a/b", "foo.", ".bar", "UPPER", "a..b", "with space", ""} {
		if validInstanceName(bad) {
			t.Errorf("validInstanceName(%q) = true, want false (E1 guard)", bad)
		}
	}
	for _, good := range []string{"t1", "test-1", "t99", "my-sandbox"} {
		if !validInstanceName(good) {
			t.Errorf("validInstanceName(%q) = false, want true", good)
		}
	}
}

func TestNewTestInstanceLayout_255(t *testing.T) {
	l := newTestInstanceLayout("/r", "t7")
	if l.CenterPrefix != filepath.Join("/r", "t7", "center") {
		t.Errorf("CenterPrefix = %q", l.CenterPrefix)
	}
	if l.Instance != "test-t7" {
		t.Errorf("Instance label = %q, want test-t7", l.Instance)
	}
	if !validInstanceName(l.Instance) {
		t.Errorf("instance label %q is not a valid launchd-safe slug", l.Instance)
	}
	if got := l.workerPrefix(2); got != filepath.Join("/r", "t7", "worker-2") {
		t.Errorf("workerPrefix(2) = %q", got)
	}
	if got := l.workerID(3); got != "test-t7-w3" {
		t.Errorf("workerID(3) = %q, want test-t7-w3", got)
	}
}

func TestRawConfigIsWorker_255(t *testing.T) {
	worker := []byte("server:\n  sqlite_path: \"/x/worker.db\"\nworker:\n  worker_id: \"w1\"\n")
	if !rawConfigIsWorker(worker) {
		t.Errorf("worker config not detected as worker")
	}
	center := []byte("server:\n  sqlite_path: \"/x/agent-center.db\"\nweb_console:\n  enabled: true\n")
	if rawConfigIsWorker(center) {
		t.Errorf("center config wrongly detected as worker")
	}
	// worker.db marker alone (no explicit worker: section) still counts.
	wdb := []byte("server:\n  sqlite_path: \"/x/worker.db\"\n")
	if !rawConfigIsWorker(wdb) {
		t.Errorf("worker.db sqlite_path not detected as worker")
	}
}

func TestDiscoverWorkerIndexes_255(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"worker-1", "worker-2", "worker-10", "center", "worker-x", "junk"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := discoverWorkerIndexes(root)
	want := []int{1, 2, 10}
	if len(got) != len(want) {
		t.Fatalf("indexes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("indexes = %v, want %v", got, want)
		}
	}
}

func TestIsWithinTestRoot_255(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, testInstanceRootName)

	ok := newTestInstanceLayout(root, "t1")
	if !isWithinTestRoot(ok) {
		t.Errorf("a direct child of the test root must be within it")
	}
	// The root itself must NOT be removable.
	rootAsLayout := testInstanceLayout{Root: root}
	if isWithinTestRoot(rootAsLayout) {
		t.Errorf("the test root itself must not pass the within-root guard")
	}
	// A path outside the test root must be rejected.
	outside := testInstanceLayout{Root: filepath.Join(home, "not-test")}
	if isWithinTestRoot(outside) {
		t.Errorf("a path outside the test root must be rejected")
	}
}

func TestTeardownTestInstance_255_ConfinedToSubtree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, testInstanceRootName)
	// Two sibling instances + a prod-looking dir that must survive.
	if err := os.MkdirAll(filepath.Join(root, "t1", "center", "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "t2", "center", "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	prod := filepath.Join(home, ".agent-center")
	if err := os.MkdirAll(prod, 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := teardownTestInstance(newTestInstanceLayout(root, "t1"), &buf); err != nil {
		t.Fatalf("teardown t1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "t1")); !os.IsNotExist(err) {
		t.Errorf("t1 subtree should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "t2")); err != nil {
		t.Errorf("sibling t2 must survive: %v", err)
	}
	if _, err := os.Stat(prod); err != nil {
		t.Errorf("prod ~/.agent-center must survive untouched: %v", err)
	}
}

func TestEmitAccessPack_255_JSONHasSelfDocumentingHints(t *testing.T) {
	pack := accessPack{
		ID:                  "t1",
		WebURL:              "http://127.0.0.1:7123",
		ServerPort:          7124,
		AdminPort:           7125,
		AdminBootstrapToken: "acat_xxx",
		WebLogin:            "no tenant seeded — use `install test-instance --with-seed` (#257) for UI signin",
		EntityIDs:           "no org/project/channel seeded — use `install test-instance --with-seed` (#257)",
		Workers:             []accessPackWorker{{ID: "test-t1-w1", Prefix: "/p/worker-1"}},
	}
	var buf bytes.Buffer
	if code := emitAccessPack(&buf, &buf, pack, "json"); code != ExitOK {
		t.Fatalf("emit json exit=%d", code)
	}
	var got accessPack
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	// The two self-documenting hint fields must be present + point at #257
	// (doc-honesty applied to the runtime artifact — no implicit gap).
	if !strings.Contains(got.WebLogin, "#257") {
		t.Errorf("web_login hint missing #257 pointer: %q", got.WebLogin)
	}
	if !strings.Contains(got.EntityIDs, "#257") {
		t.Errorf("entity_ids hint missing #257 pointer: %q", got.EntityIDs)
	}
	if got.AdminBootstrapToken == "" || got.WebURL == "" {
		t.Errorf("access pack missing core infra fields")
	}
}

func TestUninstallTestInstance_255_NoExistIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fs := flag.NewFlagSet("test-instance", flag.ContinueOnError)
	handler := uninstallTestInstanceHandler(fs)
	_ = fs.Parse([]string{"--id", "tdoesnotexist"})
	var out, errw bytes.Buffer
	if code := handler(context.Background(), fs.Args(), &out, &errw); code != ExitOK {
		t.Fatalf("uninstall non-existent should be a no-op ExitOK, got %d (err=%s)", code, errw.String())
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("expected 'not found' no-op message, got %q", out.String())
	}
}

func TestUninstallTestInstance_255_RejectsTraversalID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fs := flag.NewFlagSet("test-instance", flag.ContinueOnError)
	handler := uninstallTestInstanceHandler(fs)
	_ = fs.Parse([]string{"--id", "../../etc"})
	var out, errw bytes.Buffer
	if code := handler(context.Background(), fs.Args(), &out, &errw); code != ExitUsage {
		t.Fatalf("traversal --id must be rejected with ExitUsage, got %d", code)
	}
}
