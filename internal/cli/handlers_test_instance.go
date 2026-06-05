// handlers_test_instance.go — `agent-center install/uninstall/list test-instance`
// (v2.8 #255).
//
// A one-command test/dev sandbox: spin up an isolated agent-center topology
// (1 center + N workers) under a namespace physically separate from the
// production install (`~/.agent-center-test/<id>/` vs `~/.agent-center`), with
// dynamically-allocated free ports (skipping :7000, macOS AirPlay #161) and
// per-instance launchd labels — so multiple sandboxes coexist and a Tester can
// drive UI/acceptance without hand-allocating ports or hand-editing config.
//
// Design constraints (locked in #255 design):
//   - Reuse the REAL install codepath: install drives installCenterFresh /
//     installWorkerFresh in-process, so the generated config is the real thing
//     (incl. blob_store, #159) — never hand-rolled (constraint #1). This file
//     only chooses the namespace/ports/labels and orchestrates.
//   - Workers auto-enroll on launchd start using the center's bootstrap token
//     (scope=*) + pinned fingerprint — no org/user/seed needed. Tenant seeding
//     (signin user, org/project/channel, --with-agent) is the #257 follow-up.
//   - Cleanup is confined to the test namespace: uninstall only ever removes the
//     `~/.agent-center-test/<id>/` subtree + the matching launchd labels, and
//     `--id` is slug-validated so a caller cannot escape the root with `..` or
//     an absolute path. The production `~/.agent-center` is never touched.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/config"
)

// testInstanceRootName is the namespace root under $HOME, physically separate
// from the production prefix (~/.agent-center) so cleanup can never touch prod.
const testInstanceRootName = ".agent-center-test"

// testInstanceRoot returns ~/.agent-center-test.
func testInstanceRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("empty home dir")
	}
	return filepath.Join(home, testInstanceRootName), nil
}

// testInstanceLayout derives the per-instance paths + labels from an id.
type testInstanceLayout struct {
	ID           string // e.g. "t1"
	Root         string // <root>/<id>
	CenterPrefix string // <root>/<id>/center
	Instance     string // "test-<id>" — the launchd label component (#211 reuse)
}

func newTestInstanceLayout(root, id string) testInstanceLayout {
	base := filepath.Join(root, id)
	return testInstanceLayout{
		ID:           id,
		Root:         base,
		CenterPrefix: filepath.Join(base, "center"),
		Instance:     "test-" + id,
	}
}

func (l testInstanceLayout) workerPrefix(n int) string {
	return filepath.Join(l.Root, fmt.Sprintf("worker-%d", n))
}

// workerID is the per-worker identity → launchd label
// com.agent-center.worker.test-<id>-w<n> via applyWorkerIDToServicePaths.
func (l testInstanceLayout) workerID(n int) string {
	return fmt.Sprintf("test-%s-w%d", l.ID, n)
}

// =============================================================================
// install test-instance
// =============================================================================

// InstallTestInstanceCommand spins up an isolated 1-center + N-worker sandbox.
func InstallTestInstanceCommand() *Command {
	return &Command{
		Name:    "test-instance",
		Group:   "Admin",
		Summary: "Spin up an isolated test sandbox (1 center + N workers, dynamic ports, isolated namespace) (v2.8 #255)",
		LongHelp: "Installs a self-contained agent-center topology under " +
			"~/.agent-center-test/<id>/ with dynamically-allocated free ports and " +
			"per-instance launchd labels, reusing the real install + enroll codepath. " +
			"Workers auto-enroll on start. Prints a machine-readable access pack " +
			"(--output=json, default). --with-seed also seeds a usable tenant " +
			"(signup owner+org + 1 project + 1 channel) → signin + entity ids. " +
			"--with-agent (implies --with-seed) org-enrolls the workers into the seeded " +
			"org (control-connected), creates a real agent, and dispatches a task so it " +
			"produces tool events.",
		Examples: []string{
			"agent-center install test-instance --workers 2",
			"agent-center install test-instance --with-seed",
			"agent-center install test-instance --with-agent --workers 1",
		},
		Flags: installTestInstanceHandler,
	}
}

func installTestInstanceHandler(fs *flag.FlagSet) Handler {
	idF := fs.String("id", "", "instance id (kebab-case slug, 1-32 chars). Default: auto-allocated t<n> (lowest free).")
	workersF := fs.Int("workers", 1, "number of workers to install + auto-enroll")
	outputF := fs.String("output", "json", "output format for the access pack: json|text")
	timeoutF := fs.Int("ready-timeout", 30, "seconds to wait for the center to become healthy")
	withSeedF := fs.Bool("with-seed", false, "seed a usable tenant after install (signup user+org + 1 project + 1 channel) and emit signin + entity_ids in the access pack (#257)")
	withAgentF := fs.Bool("with-agent", false, "implies --with-seed; org-enroll the workers into the seeded org (control-connected, no 409), create 1 real agent, and dispatch a simple task so it produces tool events (#261)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		if *workersF < 0 || *workersF > 32 {
			return PrintError(errw, FormatText, "test_instance_bad_workers",
				"--workers must be between 0 and 32", ExitUsage)
		}
		format := strings.ToLower(strings.TrimSpace(*outputF))
		if format != "json" && format != "text" {
			return PrintError(errw, FormatText, "test_instance_bad_output",
				"--output must be json or text", ExitUsage)
		}
		root, err := testInstanceRoot()
		if err != nil {
			return PrintError(errw, FormatText, "test_instance_root_failed", err.Error(), ExitBusinessError)
		}

		// Resolve the instance id: explicit (slug-validated, E1) or auto-allocated
		// (race-safe via atomic mkdir of the namespace dir).
		var id string
		explicit := strings.TrimSpace(*idF)
		if explicit != "" {
			if !validInstanceName(explicit) {
				return PrintError(errw, FormatText, "test_instance_bad_id",
					"--id must be kebab-case (a-z, 0-9, single interior dashes), 1-32 chars — no '..' / '/' / absolute paths", ExitUsage)
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				return PrintError(errw, FormatText, "test_instance_root_failed", err.Error(), ExitBusinessError)
			}
			dir := filepath.Join(root, explicit)
			if mkErr := os.Mkdir(dir, 0o755); mkErr != nil {
				if os.IsExist(mkErr) {
					return PrintError(errw, FormatText, "test_instance_exists",
						fmt.Sprintf("test-instance %q already exists at %s — pick another --id or uninstall it first", explicit, dir), ExitBusinessError)
				}
				return PrintError(errw, FormatText, "test_instance_mkdir_failed", mkErr.Error(), ExitBusinessError)
			}
			id = explicit
		} else {
			alloc, aerr := allocateTestInstanceID(root)
			if aerr != nil {
				return PrintError(errw, FormatText, "test_instance_alloc_failed", aerr.Error(), ExitBusinessError)
			}
			id = alloc
		}

		layout := newTestInstanceLayout(root, id)
		timeout := time.Duration(*timeoutF) * time.Second
		// #261 --with-agent REORDERS: the center is provisioned alone first; the
		// workers are installed later (org-bound, after the tenant org is seeded)
		// so they enroll INTO the org and control-connect (no 409). Without
		// --with-agent, provision the center + N bootstrap-token workers (#255).
		provisionWorkers := *workersF
		if *withAgentF {
			provisionWorkers = 0
		}
		pack, ierr := provisionTestInstance(ctx, layout, provisionWorkers, timeout)
		if ierr != nil {
			// Best-effort rollback so a failed provision doesn't strand a half-built
			// namespace (D5). Confined to this instance's subtree + labels.
			_ = teardownTestInstance(layout, errw)
			return PrintError(errw, FormatText, "test_instance_provision_failed", ierr.Error(), ExitBusinessError)
		}
		// #257 --with-seed (tenant only): drive a usable tenant via the REAL center
		// API (signup → project → channel) so the access pack carries signin +
		// entity_ids for 0-round-trip UI consumption.
		if *withSeedF && !*withAgentF {
			if _, _, serr := seedTestInstanceTenant(ctx, &pack); serr != nil {
				_ = teardownTestInstance(layout, errw)
				return PrintError(errw, FormatText, "test_instance_seed_failed", serr.Error(), ExitBusinessError)
			}
		}
		// #261 --with-agent: seed tenant + org-enroll workers (control-connected) +
		// create a real agent + dispatch a simple task so it produces tool events.
		if *withAgentF {
			if serr := seedAndEnrollWithAgent(ctx, &pack, layout, *workersF, timeout); serr != nil {
				_ = teardownTestInstance(layout, errw)
				return PrintError(errw, FormatText, "test_instance_with_agent_failed", serr.Error(), ExitBusinessError)
			}
		}
		return emitAccessPack(out, errw, pack, format)
	}
}

// allocateTestInstanceID picks the lowest-free t<n> and claims it by atomically
// creating its namespace dir (race-safe: two concurrent installs that both try
// t1 → one wins, the loser gets EEXIST and advances to t2).
func allocateTestInstanceID(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for n := 1; n <= 9999; n++ {
		id := "t" + strconv.Itoa(n)
		if err := os.Mkdir(filepath.Join(root, id), 0o755); err == nil {
			return id, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("no free test-instance id in t1..t9999")
}

// =============================================================================
// provisioning — reuse the real install + enroll codepath
// =============================================================================

// accessPack is the machine-readable output (#255 infra-piggyback + the
// self-documenting #257 pointer hints).
type accessPack struct {
	ID                 string            `json:"id"`
	Prefix             string            `json:"prefix"`
	WebURL             string            `json:"web_url"`
	ServerPort         int               `json:"server_port"`
	AdminPort          int               `json:"admin_port"`
	AdminBootstrapToken string           `json:"admin_bootstrap_token"`
	Workers            []accessPackWorker `json:"workers"`
	// Self-documenting hints — doc-honesty applied to the runtime artifact:
	// a consumer reads the JSON and sees exactly why signin / entities are
	// absent + why workers aren't control-connected yet, and where to get
	// them (#257), instead of guessing at a missing key / a 409.
	WebLogin    string `json:"web_login"`
	EntityIDs   string `json:"entity_ids"`
	WorkersNote string `json:"workers_note"`
	// Populated only by --with-seed (#257): a usable signin + the seeded entity
	// ids, so a consumer (human or agent) can log in + navigate with zero
	// round-trips. Omitted when not seeded (the hints above explain why).
	Signin   *seedSignin   `json:"signin,omitempty"`
	Entities *seedEntities `json:"entity_refs,omitempty"`
	// Populated only by --with-agent (#261): the real agent created + the task
	// dispatched to it (so it produces tool events on a control-connected worker).
	Agent *seedAgent `json:"agent,omitempty"`
}

type accessPackWorker struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	HealthURL string `json:"health_url,omitempty"`
}

type seedAgent struct {
	ID               string `json:"id"`
	CLI              string `json:"cli"`
	Model            string `json:"model"`
	WorkerID         string `json:"worker_id"`
	DispatchedTaskID string `json:"dispatched_task_id,omitempty"`
}

type seedSignin struct {
	OrgSlug     string `json:"org_slug"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Passcode    string `json:"passcode"`
}

type seedEntities struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	ChannelID string `json:"channel_id"`
}

// provisionTestInstance installs + starts the center, waits for health, reads
// the bootstrap token + fingerprint, then installs + auto-enrolls N workers.
func provisionTestInstance(ctx context.Context, layout testInstanceLayout, workers int, readyTimeout time.Duration) (accessPack, error) {
	version := installerVersion()

	webPort, serverPort, adminPort, err := allocateThreeFreePorts()
	if err != nil {
		return accessPack{}, fmt.Errorf("allocate ports: %w", err)
	}
	adminTCP := fmt.Sprintf("127.0.0.1:%d", adminPort)

	// Install + start the center via the REAL codepath (Service=true =
	// LaunchAgent, matching a real install — constraint #1). Capture its human
	// output so it doesn't pollute the JSON access pack.
	var sink bytes.Buffer
	if code := installCenterFresh(&sink, &sink, installContext{
		Prefix:     layout.CenterPrefix,
		UserMode:   isMacRuntime(),
		Instance:   layout.Instance,
		Port:       webPort,
		ServerPort: serverPort,
		TCPListen:  adminTCP,
		Version:    version,
		Service:    true,
	}); code != ExitOK {
		return accessPack{}, fmt.Errorf("install center failed: %s", strings.TrimSpace(sink.String()))
	}

	centerLayout := newInstallLayout(layout.CenterPrefix, version)
	webURL := fmt.Sprintf("http://127.0.0.1:%d", webPort)

	// Wait until the center's web console answers /api/health — this also proves
	// the bootstrap token + fingerprint files have been written at boot.
	if err := waitCenterHealthy(ctx, webURL, readyTimeout); err != nil {
		return accessPack{}, fmt.Errorf("center did not become healthy: %w", err)
	}

	bootstrapToken, err := readBootstrapToken(centerLayout.DataDir)
	if err != nil {
		return accessPack{}, fmt.Errorf("read bootstrap token: %w", err)
	}
	fingerprint, err := readCenterFingerprint(centerLayout.ConfigPath)
	if err != nil {
		return accessPack{}, fmt.Errorf("read server fingerprint: %w", err)
	}

	pack := accessPack{
		ID:                  layout.ID,
		Prefix:              layout.Root,
		WebURL:              webURL,
		ServerPort:          serverPort,
		AdminPort:           adminPort,
		AdminBootstrapToken: bootstrapToken,
		WebLogin:            "no tenant seeded — use `install test-instance --with-seed` (#257) for UI signin",
		EntityIDs:           "no org/project/channel seeded — use `install test-instance --with-seed` (#257)",
		WorkersNote:         "workers are workforce-enrolled (token exchanged) + running, but NOT org-enrolled yet — control-connect 409s until a tenant org is seeded (--with-seed, #257)",
	}

	// Install + start each worker via the real codepath. The worker daemon
	// auto-enrolls on launchd start using the bootstrap token (scope=* covers
	// workforce:enroll) + the pinned fingerprint, then exchanges it for its own
	// long-term token (workerdaemon EnrollWithExchange) — no org/user needed.
	for n := 1; n <= workers; n++ {
		wid := layout.workerID(n)
		wPrefix := layout.workerPrefix(n)
		var wsink bytes.Buffer
		if code := installWorkerFresh(&wsink, &wsink, installContext{
			Prefix:      wPrefix,
			UserMode:    isMacRuntime(),
			WorkerID:    wid,
			WorkerName:  wid,
			Bootstrap:   fmt.Sprintf("tcp://127.0.0.1:%d", adminPort),
			Token:       bootstrapToken,
			Fingerprint: fingerprint,
			Version:     version,
			Service:     true,
		}); code != ExitOK {
			return accessPack{}, fmt.Errorf("install worker %s failed: %s", wid, strings.TrimSpace(wsink.String()))
		}
		pack.Workers = append(pack.Workers, accessPackWorker{ID: wid, Prefix: wPrefix})
	}
	return pack, nil
}

// allocateThreeFreePorts returns three distinct loopback ports, never :7000
// (macOS AirPlay #161). Uses OS-assigned ephemeral ports (net.Listen ":0") so
// they are free at probe time; installCenterFresh re-checks via preflight.
func allocateThreeFreePorts() (web, server, admin int, err error) {
	seen := map[int]bool{}
	got := make([]int, 0, 3)
	for len(got) < 3 {
		p, perr := probeFreePort()
		if perr != nil {
			return 0, 0, 0, perr
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		got = append(got, p)
	}
	return got[0], got[1], got[2], nil
}

func probeFreePort() (int, error) {
	for attempts := 0; attempts < 100; attempts++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if port == 7000 { // #161: never hand out the AirPlay port
			continue
		}
		return port, nil
	}
	return 0, errors.New("could not find a free loopback port")
}

// waitCenterHealthy polls GET <webURL>/api/health until 200 or timeout.
func waitCenterHealthy(ctx context.Context, webURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := strings.TrimRight(webURL, "/") + "/api/health"
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("timeout")
	}
	return lastErr
}

// readBootstrapToken reads the admin bootstrap token the center mints on its
// first boot (EnsureBootstrapToken → <datadir>/bootstrap_token, 0600).
func readBootstrapToken(dataDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, BootstrapTokenFilename))
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", errors.New("bootstrap token file empty")
	}
	return tok, nil
}

// readCenterFingerprint loads the center config to derive the admin-tls
// fingerprint file path (defaultTLSDir(sqlite_path)/admin-tls.fingerprint) and
// reads the SSH-style fingerprint the worker must pin.
func readCenterFingerprint(configPath string) (string, error) {
	cfg, err := config.Load(config.LoadOptions{Path: configPath})
	if err != nil {
		return "", fmt.Errorf("load center config: %w", err)
	}
	fpPath := filepath.Join(defaultTLSDir(cfg.Server.SqlitePath), "admin-tls.fingerprint")
	raw, err := os.ReadFile(fpPath)
	if err != nil {
		return "", fmt.Errorf("read fingerprint %s: %w", fpPath, err)
	}
	fp := strings.TrimSpace(string(raw))
	if fp == "" {
		return "", errors.New("fingerprint file empty")
	}
	return fp, nil
}

// emitAccessPack writes the access pack as JSON (default, machine-readable) or
// a human-readable summary.
func emitAccessPack(out, errw io.Writer, pack accessPack, format string) ExitCode {
	if format == "text" {
		fmt.Fprintf(out, "✓ test-instance %s up\n", pack.ID)
		fmt.Fprintf(out, "  prefix:    %s\n", pack.Prefix)
		fmt.Fprintf(out, "  web url:   %s\n", pack.WebURL)
		fmt.Fprintf(out, "  ports:     server=%d admin=%d\n", pack.ServerPort, pack.AdminPort)
		fmt.Fprintf(out, "  workers:   %d\n", len(pack.Workers))
		for _, w := range pack.Workers {
			fmt.Fprintf(out, "    - %s (%s)\n", w.ID, w.Prefix)
		}
		fmt.Fprintf(out, "  web login: %s\n", pack.WebLogin)
		return ExitOK
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pack); err != nil {
		return PrintError(errw, FormatText, "test_instance_encode_failed", err.Error(), ExitBusinessError)
	}
	return ExitOK
}

// =============================================================================
// uninstall test-instance — namespace-confined teardown
// =============================================================================

// UninstallTestInstanceCommand tears down a sandbox: bootout the launchd labels
// + remove the namespace subtree. Confined to ~/.agent-center-test/<id>/.
func UninstallTestInstanceCommand() *Command {
	return &Command{
		Name:    "test-instance",
		Group:   "Admin",
		Summary: "Tear down a test sandbox (bootout labels + remove its namespace subtree) (v2.8 #255)",
		Examples: []string{
			"agent-center uninstall test-instance --id t1",
		},
		Flags: uninstallTestInstanceHandler,
	}
}

func uninstallTestInstanceHandler(fs *flag.FlagSet) Handler {
	idF := fs.String("id", "", "instance id to tear down (required)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		id := strings.TrimSpace(*idF)
		if id == "" {
			return PrintError(errw, FormatText, "test_instance_id_required", "--id is required", ExitUsage)
		}
		// E1: slug-validate so a caller cannot escape the test root with '..' /
		// '/' / an absolute path — uninstall is physically confined to the root.
		if !validInstanceName(id) {
			return PrintError(errw, FormatText, "test_instance_bad_id",
				"--id must be kebab-case (a-z, 0-9, single interior dashes), 1-32 chars", ExitUsage)
		}
		root, err := testInstanceRoot()
		if err != nil {
			return PrintError(errw, FormatText, "test_instance_root_failed", err.Error(), ExitBusinessError)
		}
		layout := newTestInstanceLayout(root, id)
		if _, statErr := os.Stat(layout.Root); statErr != nil {
			if os.IsNotExist(statErr) {
				// D4: tearing down a non-existent instance is a safe no-op.
				fmt.Fprintf(out, "test-instance %s not found — nothing to do\n", id)
				return ExitOK
			}
			return PrintError(errw, FormatText, "test_instance_stat_failed", statErr.Error(), ExitBusinessError)
		}
		if err := teardownTestInstance(layout, out); err != nil {
			return PrintError(errw, FormatText, "test_instance_teardown_failed", err.Error(), ExitBusinessError)
		}
		fmt.Fprintf(out, "✓ test-instance %s torn down (labels booted out + %s removed)\n", id, layout.Root)
		return ExitOK
	}
}

// teardownTestInstance boots out every launchd/systemd label belonging to the
// instance (center + any worker-N present on disk) then removes the namespace
// subtree. Confined to layout.Root — it never touches the prod prefix.
// workerIDFromConfig reads the installed worker's real worker_id from its
// config (worker.worker_id). Empty when the config is absent/unreadable.
func workerIDFromConfig(workerPrefix string) string {
	cfgPath := newInstallLayout(workerPrefix, "").ConfigPath
	cfg, err := config.Load(config.LoadOptions{Path: cfgPath})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Worker.WorkerID)
}

func teardownTestInstance(layout testInstanceLayout, out io.Writer) error {
	home, _ := os.UserHomeDir()
	sp, perr := platformPaths(runtimeOS(), isMacRuntime(), home)
	if perr == nil {
		// Center label (com.agent-center.center.test-<id>).
		centerSP := applyInstanceToServicePaths(sp, layout.Instance)
		bootoutUnit(centerSP, centerSP.CenterServiceID, centerSP.CenterUnitPath, out)
		// Worker labels — discover worker-N subdirs on disk and bootout each by
		// its REAL worker_id read from its config (#261: --with-agent workers carry
		// a server-assigned worker-<hex> id from mint-enroll, not test-<id>-w<n>;
		// reading the config covers both that and the #255 computed id).
		for _, n := range discoverWorkerIndexes(layout.Root) {
			wid := workerIDFromConfig(layout.workerPrefix(n))
			if wid == "" {
				wid = layout.workerID(n) // fallback to the computed id
			}
			wsp := applyWorkerIDToServicePaths(sp, wid)
			bootoutUnit(wsp, wsp.WorkerServiceID, wsp.WorkerUnitPath, out)
		}
	}
	// Safety belt: only ever remove inside the test root.
	if !isWithinTestRoot(layout) {
		return fmt.Errorf("refusing to remove %q: not within the test-instance root", layout.Root)
	}
	if err := os.RemoveAll(layout.Root); err != nil {
		return fmt.Errorf("remove %s: %w", layout.Root, err)
	}
	return nil
}

// bootoutUnit deactivates a service (launchctl bootout / systemctl stop+disable)
// and removes its unit file. Best-effort + tolerant — the caller wants
// everything gone, so a not-loaded service or a missing file is not an error.
// Reuses the same teardown command set as `uninstall center/worker`.
func bootoutUnit(sp servicePaths, serviceID, unitPath string, out io.Writer) {
	if !unitFileExists(unitPath) {
		return
	}
	for _, s := range serviceTeardownCmds(sp, serviceID) {
		runShellTolerant(out, s)
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "warning: remove unit file %s: %v\n", unitPath, err)
	}
}

// isWithinTestRoot is a defense-in-depth check that layout.Root resolves to a
// direct child of ~/.agent-center-test (belt-and-suspenders on top of the slug
// validation) — so RemoveAll can never escape the namespace.
func isWithinTestRoot(layout testInstanceLayout) bool {
	root, err := testInstanceRoot()
	if err != nil {
		return false
	}
	rootClean := filepath.Clean(root)
	target := filepath.Clean(layout.Root)
	return filepath.Dir(target) == rootClean && target != rootClean
}

// discoverWorkerIndexes returns the n's of worker-<n> subdirs present under the
// instance root.
func discoverWorkerIndexes(instanceRoot string) []int {
	entries, err := os.ReadDir(instanceRoot)
	if err != nil {
		return nil
	}
	var ns []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if rest, ok := strings.CutPrefix(e.Name(), "worker-"); ok {
			if n, perr := strconv.Atoi(rest); perr == nil {
				ns = append(ns, n)
			}
		}
	}
	sort.Ints(ns)
	return ns
}

// =============================================================================
// list test-instances
// =============================================================================

// ListTestInstancesCommand lists the sandboxes under ~/.agent-center-test.
// Named with the `list-` prefix for symmetry with list-local-centers (#211) /
// list-local-workers (#170).
func ListTestInstancesCommand() *Command {
	return &Command{
		Name:    "list-test-instances",
		Group:   "Admin",
		Summary: "List test sandboxes installed under ~/.agent-center-test (v2.8 #255)",
		Flags:   listTestInstancesHandler,
	}
}

type testInstanceRow struct {
	ID      string `json:"id"`
	Prefix  string `json:"prefix"`
	WebPort string `json:"web_port"`
	Workers int    `json:"workers"`
	Online  string `json:"online"`
}

func listTestInstancesHandler(fs *flag.FlagSet) Handler {
	outputF := fs.String("output", "text", "output format: text|json")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		rows, err := discoverTestInstances()
		if err != nil {
			return PrintError(errw, FormatText, "list_test_instances_failed", err.Error(), ExitBusinessError)
		}
		if strings.ToLower(strings.TrimSpace(*outputF)) == "json" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rows); err != nil {
				return PrintError(errw, FormatText, "list_test_instances_encode_failed", err.Error(), ExitBusinessError)
			}
			return ExitOK
		}
		if len(rows) == 0 {
			fmt.Fprintln(out, "No test-instances found under ~/.agent-center-test.")
			return ExitOK
		}
		fmt.Fprintf(out, "%-12s %-40s %-6s %-8s %s\n", "ID", "PREFIX", "WEB", "WORKERS", "ONLINE")
		for _, r := range rows {
			fmt.Fprintf(out, "%-12s %-40s %-6s %-8d %s\n", r.ID, r.Prefix, r.WebPort, r.Workers, r.Online)
		}
		return ExitOK
	}
}

func discoverTestInstances() ([]testInstanceRow, error) {
	root, err := testInstanceRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []testInstanceRow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		layout := newTestInstanceLayout(root, id)
		centerLayout := newInstallLayout(layout.CenterPrefix, "")
		cfgPath := centerLayout.ConfigPath
		if _, statErr := os.Stat(cfgPath); statErr != nil {
			continue // not a provisioned instance (e.g. a bare placeholder)
		}
		row := testInstanceRow{ID: id, Prefix: layout.Root, WebPort: "-", Online: "?"}
		row.Workers = len(discoverWorkerIndexes(layout.Root))
		if cfg, lerr := config.Load(config.LoadOptions{Path: cfgPath}); lerr == nil {
			row.WebPort = portOf(cfg.WebConsole.ListenAddr)
			row.Online = centerOnline(cfg.WebConsole.ListenAddr)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}
