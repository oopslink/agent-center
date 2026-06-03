package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v2.4-D-A2 tests for platform paths + unit-file rendering + actual
// install (with activation skipped via env var).

func TestPlatformPaths_Darwin(t *testing.T) {
	home := "/Users/test"
	sp, err := platformPaths("darwin", true, home)
	if err != nil {
		t.Fatal(err)
	}
	if sp.ServiceManager != "launchd" {
		t.Errorf("manager = %q", sp.ServiceManager)
	}
	if !strings.HasPrefix(sp.CenterUnitPath, home+"/Library/LaunchAgents/") {
		t.Errorf("center plist = %q", sp.CenterUnitPath)
	}
	if sp.CenterServiceID != "com.agent-center.center" {
		t.Errorf("center id = %q", sp.CenterServiceID)
	}
}

func TestPlatformPaths_LinuxUser(t *testing.T) {
	home := "/home/test"
	sp, err := platformPaths("linux", true, home)
	if err != nil {
		t.Fatal(err)
	}
	if sp.ServiceManager != "systemd" {
		t.Errorf("manager = %q", sp.ServiceManager)
	}
	if !strings.Contains(sp.CenterUnitPath, "/.config/systemd/user/") {
		t.Errorf("center unit = %q", sp.CenterUnitPath)
	}
}

func TestPlatformPaths_LinuxSystem(t *testing.T) {
	sp, err := platformPaths("linux", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if sp.CenterUnitPath != "/etc/systemd/system/agent-center.service" {
		t.Errorf("system unit = %q", sp.CenterUnitPath)
	}
}

func TestPlatformPaths_UnsupportedOS(t *testing.T) {
	_, err := platformPaths("windows", false, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported OS") {
		t.Fatalf("expected unsupported OS error, got %v", err)
	}
}

func TestRenderSystemdUnit_Worker_HasKillModeProcess(t *testing.T) {
	body := renderSystemdUnit(systemdUnit{
		Description:  "worker",
		ExecStart:    "/bin/x",
		KillMode:     "process",
		UserMode:     true,
		WantedByUser: "default.target",
	})
	if !strings.Contains(body, "KillMode=process") {
		t.Errorf("missing KillMode: %s", body)
	}
	if !strings.Contains(body, "WantedBy=default.target") {
		t.Errorf("user mode WantedBy: %s", body)
	}
}

func TestRenderSystemdUnit_Center_SystemMode_HasHardening(t *testing.T) {
	body := renderSystemdUnit(systemdUnit{
		Description: "center",
		ExecStart:   "/bin/x",
		UserMode:    false,
	})
	if !strings.Contains(body, "NoNewPrivileges=true") {
		t.Errorf("missing hardening: %s", body)
	}
}

func TestRenderLaunchdPlist_HasLabel(t *testing.T) {
	body := renderLaunchdPlist("com.test.x", "/bin/agent-center", []string{"server", "--config=/tmp/c.yaml"}, "/var/logs/ac", "")
	if !strings.Contains(body, "com.test.x") {
		t.Errorf("missing label: %s", body)
	}
	if !strings.Contains(body, "/bin/agent-center") {
		t.Errorf("missing binary: %s", body)
	}
	if !strings.Contains(body, "<string>--config=/tmp/c.yaml</string>") {
		t.Errorf("missing arg: %s", body)
	}
	if !strings.Contains(body, "KeepAlive") {
		t.Errorf("missing KeepAlive: %s", body)
	}
	// v2.4.1: stdout/stderr land under the per-install logs dir so a
	// `~/.agent-center/` install keeps the daemon log next to the rest
	// of the install (no more /tmp scavenging on reboot).
	if !strings.Contains(body, "<string>/var/logs/ac/com.test.x.err.log</string>") {
		t.Errorf("StandardErrorPath not under logsDir:\n%s", body)
	}
	if !strings.Contains(body, "<string>/var/logs/ac/com.test.x.out.log</string>") {
		t.Errorf("StandardOutPath not under logsDir:\n%s", body)
	}
}

// renderLaunchdPlist falls back to /tmp when logsDir is empty — the
// safety net for test fixtures that don't bother constructing a layout.
func TestRenderLaunchdPlist_EmptyLogsDirFallsBackToTmp(t *testing.T) {
	body := renderLaunchdPlist("com.test.x", "/bin/x", nil, "", "")
	if !strings.Contains(body, "<string>/tmp/com.test.x.err.log</string>") {
		t.Errorf("expected /tmp fallback for empty logsDir:\n%s", body)
	}
	// Empty pathEnv (center / no-PATH case) must NOT emit an
	// EnvironmentVariables block.
	if strings.Contains(body, "EnvironmentVariables") {
		t.Errorf("empty pathEnv should not emit EnvironmentVariables:\n%s", body)
	}
}

func TestCenterConfigYAML_HasFields(t *testing.T) {
	yaml := centerConfigYAML("/var/data", 7100, "", "/var/data/master.key")
	for _, want := range []string{
		"sqlite_path:",
		"admin_socket_path:",
		"listen_addr",
		"web_console:",
		"127.0.0.1:7100",
		// v2.5 X1 polish: master_key auto-provisioned at install time.
		`master_key_file: "/var/data/master.key"`,
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("missing %q in yaml:\n%s", want, yaml)
		}
	}
	if strings.Contains(yaml, "admin_tcp_listen:") {
		t.Errorf("unexpected admin_tcp_listen for empty arg:\n%s", yaml)
	}
}

func TestCenterConfigYAML_WithTCPListen(t *testing.T) {
	yaml := centerConfigYAML("/var/data", 7100, "0.0.0.0:7300", "/var/data/master.key")
	if !strings.Contains(yaml, `admin_tcp_listen: "0.0.0.0:7300"`) {
		t.Errorf("missing admin_tcp_listen:\n%s", yaml)
	}
}

// v2.5 X1 polish: writeCenterConfig must auto-generate the master_key
// file (mode 0600) on first install and reuse it on re-install so the
// AdminToken Show install command + UserSecret BC don't need the
// operator to manually provision one.
func TestWriteCenterConfig_AutoProvisionsMasterKey(t *testing.T) {
	prefix := t.TempDir()
	layout := newInstallLayout(prefix, "v2.5.0")
	if err := writeCenterConfig(layout, 7100, "0.0.0.0:7300"); err != nil {
		t.Fatalf("writeCenterConfig: %v", err)
	}
	keyPath := filepath.Join(layout.DataDir, "master.key")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("master key not provisioned: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("master key perms = %o, want 0600", mode)
	}
	original, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) == 0 {
		t.Fatal("master key file empty")
	}
	// Re-install should preserve the existing key (otherwise every
	// upgrade would orphan all encrypted UserSecret payloads).
	if err := writeCenterConfig(layout, 7100, "0.0.0.0:7300"); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	preserved, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(preserved) {
		t.Fatalf("master key rotated on re-install — data loss risk")
	}
}

func TestNewInstallLayout(t *testing.T) {
	layout := newInstallLayout("/opt/ac", "v2.4.0")
	if layout.VersionedDir != "/opt/ac/versions/v2.4.0" {
		t.Errorf("versioned dir = %q", layout.VersionedDir)
	}
	if layout.CurrentLink != "/opt/ac/current" {
		t.Errorf("current link = %q", layout.CurrentLink)
	}
	if layout.CurrentBinDir != "/opt/ac/current/bin" {
		t.Errorf("current bin dir = %q", layout.CurrentBinDir)
	}
}

func TestAtomicSymlinkSwap(t *testing.T) {
	prefix := t.TempDir()
	verDir := filepath.Join(prefix, "versions", "v2.4.0")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	layout := newInstallLayout(prefix, "v2.4.0")
	if err := atomicSymlinkSwap(layout); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(layout.CurrentLink)
	if err != nil {
		t.Fatal(err)
	}
	if target != verDir {
		t.Errorf("link target = %q, want %q", target, verDir)
	}
	// Re-swap (different version) should work without leftover .new file.
	verDir2 := filepath.Join(prefix, "versions", "v2.4.1")
	if err := os.MkdirAll(verDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	layout2 := newInstallLayout(prefix, "v2.4.1")
	if err := atomicSymlinkSwap(layout2); err != nil {
		t.Fatal(err)
	}
	target, _ = os.Readlink(layout.CurrentLink)
	if target != verDir2 {
		t.Errorf("after re-swap, link target = %q, want %q", target, verDir2)
	}
	if _, err := os.Stat(layout.CurrentLink + ".new"); !os.IsNotExist(err) {
		t.Errorf(".new file leftover")
	}
}

func TestWriteVersionFile_RoundTrip(t *testing.T) {
	prefix := t.TempDir()
	verDir := filepath.Join(prefix, "versions", "v2.4.0")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	layout := newInstallLayout(prefix, "v2.4.0")
	if err := writeVersionFile(layout); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(verDir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != "v2.4.0" {
		t.Errorf("VERSION body = %q", body)
	}
}

func TestInstallShouldActivate_EnvOverride(t *testing.T) {
	sp := servicePaths{ServiceManager: "launchd"}
	t.Setenv("AGENT_CENTER_INSTALL_SKIP_ACTIVATE", "1")
	if installShouldActivate(sp) {
		t.Error("env override should disable activation")
	}
}

func TestInstallShouldActivate_UnknownManager(t *testing.T) {
	if installShouldActivate(servicePaths{ServiceManager: ""}) {
		t.Error("unknown manager should disable activation")
	}
}

func TestSplitSpaces(t *testing.T) {
	got := splitSpaces("systemctl --user enable foo.service")
	if len(got) != 4 {
		t.Fatalf("len=%d %v", len(got), got)
	}
	if got[0] != "systemctl" || got[3] != "foo.service" {
		t.Errorf("got %v", got)
	}
}

// v2.7 (b) cutover: the worker service unit now runs the UNIFIED `agent-center`
// binary as `agent-center worker run ...`, so the `worker run` sub-command prefix
// IS present (reversing the v2.4-D-F4 X1 expectation — the unified CLI router
// consumes the sub-command path before flag parsing, so the prefix is correct).
func TestRenderWorkerServiceUnit_HasWorkerRunPrefix(t *testing.T) {
	sp := servicePaths{
		ServiceManager:  "launchd",
		WorkerServiceID: "com.agent-center.worker",
		WorkerUnitPath:  "/tmp/test.plist",
		UserMode:        true,
	}
	body := renderWorkerServiceUnit(sp, "/opt/agent-center/current/bin/agent-center",
		"/opt/agent-center/config.yaml",
		"my-worker", "My Test Worker", "tcp://host:7300", "tok-abc", "sha256:AA", "/opt/agent-center/logs",
		"/opt/homebrew/bin:/usr/bin")
	// The `worker run` sub-command path must be present (as ordered plist args).
	if !strings.Contains(body, "<string>worker</string>") {
		t.Errorf("plist missing 'worker' sub-command prefix:\n%s", body)
	}
	if !strings.Contains(body, "<string>run</string>") {
		t.Errorf("plist missing 'run' sub-command prefix:\n%s", body)
	}
	// All required flags present.
	for _, want := range []string{
		"--worker-id=my-worker",
		"--admin-target=tcp://host:7300",
		"--admin-token=tok-abc",
		"--server-fingerprint=sha256:AA",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q:\n%s", want, body)
		}
	}
	// v2.7 #147: the unit must NOT bake in --capabilities — the daemon
	// auto-probes installed CLIs on every online instead.
	if strings.Contains(body, "--capabilities=") {
		t.Errorf("unit must not carry --capabilities (auto-probe now):\n%s", body)
	}
	// v2.7 #175: the worker plist must carry an EnvironmentVariables PATH so
	// the launchd-started daemon can find user-installed agent CLIs.
	if !strings.Contains(body, "<key>EnvironmentVariables</key>") {
		t.Errorf("worker plist missing EnvironmentVariables block:\n%s", body)
	}
	if !strings.Contains(body, "<key>PATH</key>") ||
		!strings.Contains(body, "<string>/opt/homebrew/bin:/usr/bin</string>") {
		t.Errorf("worker plist missing/incorrect PATH:\n%s", body)
	}
}

func TestRenderWorkerServiceUnit_OmitsEmptyOptionals(t *testing.T) {
	sp := servicePaths{
		ServiceManager:  "launchd",
		WorkerServiceID: "com.agent-center.worker",
		WorkerUnitPath:  "/tmp/test.plist",
	}
	body := renderWorkerServiceUnit(sp, "/opt/x", "/opt/cfg.yaml",
		"w", "" /* no name */, "unix:/run/admin.sock", "tok", "" /* no fingerprint */, "/opt/logs", "")
	if strings.Contains(body, "--server-fingerprint=") {
		t.Errorf("empty fingerprint should be omitted:\n%s", body)
	}
	if strings.Contains(body, "--capabilities=") {
		t.Errorf("empty capabilities should be omitted:\n%s", body)
	}
	if strings.Contains(body, "--worker-name=") {
		t.Errorf("empty worker-name should be omitted:\n%s", body)
	}
}

// v2.7 #175: the systemd worker unit must carry Environment=PATH= so the
// daemon (and the agent CLIs it spawns) inherit the user's PATH.
func TestRenderWorkerServiceUnit_SystemdHasPathEnv(t *testing.T) {
	sp := servicePaths{
		ServiceManager:  "systemd",
		WorkerServiceID: "agent-center-worker-w.service",
		UserMode:        true,
	}
	body := renderWorkerServiceUnit(sp, "/opt/x", "/opt/cfg.yaml",
		"w", "", "unix:/run/admin.sock", "tok", "", "/opt/logs",
		"/home/u/.local/bin:/usr/bin")
	if !strings.Contains(body, "Environment=PATH=/home/u/.local/bin:/usr/bin") {
		t.Errorf("systemd worker unit missing Environment=PATH:\n%s", body)
	}
}

// v2.7 #175: resolveWorkerPATH unions the live PATH (first, in order)
// with well-known user CLI dirs, de-duplicating while preserving order.
func TestResolveWorkerPATH_UnionDedupPreserveOrder(t *testing.T) {
	// /opt/homebrew/bin appears in both the live PATH and the well-known
	// backstop — it must not be duplicated.
	t.Setenv("PATH", "/custom/bin:/opt/homebrew/bin")
	parts := filepath.SplitList(resolveWorkerPATH("/home/u"))

	if len(parts) < 2 || parts[0] != "/custom/bin" || parts[1] != "/opt/homebrew/bin" {
		t.Fatalf("live PATH must come first in order, got: %v", parts)
	}
	count := map[string]int{}
	for _, p := range parts {
		count[p]++
	}
	if count["/opt/homebrew/bin"] != 1 {
		t.Fatalf("/opt/homebrew/bin must be de-duplicated, got %d: %v", count["/opt/homebrew/bin"], parts)
	}
	for _, want := range []string{
		"/usr/local/bin", "/home/u/.local/bin", "/home/u/.cargo/bin",
		"/home/u/.npm-global/bin", "/home/u/.volta/bin",
	} {
		if count[want] != 1 {
			t.Errorf("missing well-known dir %q in: %v", want, parts)
		}
	}
}

// v2.4-D-X1 multi-worker: launchd Label + plist path must be scoped
// by worker-id so two workers on one machine don't collide.
func TestApplyWorkerIDToServicePaths_Launchd(t *testing.T) {
	base, err := platformPaths("darwin", true, "/Users/test")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		workerID string
		wantSvc  string
	}{
		{"w-alpha", "com.agent-center.worker.w-alpha"},
		{"Tenant Foo", "com.agent-center.worker.tenant-foo"},
		{"!!!", "com.agent-center.worker.default"},
		{"", "com.agent-center.worker.default"},
	} {
		got := applyWorkerIDToServicePaths(base, c.workerID)
		if got.WorkerServiceID != c.wantSvc {
			t.Errorf("workerID=%q got=%q want=%q", c.workerID, got.WorkerServiceID, c.wantSvc)
		}
		if !strings.HasSuffix(got.WorkerUnitPath, "/"+c.wantSvc+".plist") {
			t.Errorf("workerID=%q plist path missing suffix: %q", c.workerID, got.WorkerUnitPath)
		}
		// CenterServiceID and CenterUnitPath untouched.
		if got.CenterServiceID != base.CenterServiceID {
			t.Errorf("center id mutated: %q", got.CenterServiceID)
		}
	}
}

func TestApplyWorkerIDToServicePaths_Systemd(t *testing.T) {
	base, _ := platformPaths("linux", true, "/home/test")
	got := applyWorkerIDToServicePaths(base, "tenant-x")
	if got.WorkerServiceID != "agent-center-worker-tenant-x.service" {
		t.Errorf("svc id = %q", got.WorkerServiceID)
	}
	if !strings.HasSuffix(got.WorkerUnitPath, "/agent-center-worker-tenant-x.service") {
		t.Errorf("unit path = %q", got.WorkerUnitPath)
	}
}

// Two installs on one machine with different --worker-id must yield
// non-overlapping prefixes + non-overlapping launchd labels.
//
// v2.4.1: worker subtree relocated to `<base>/workers/<id>/` so the
// center install and all per-worker installs nest under one ~/.agent-
// center tree instead of scattering peer worker-<id> dirs at the
// home root.
func TestDefaultWorkerInstallPrefix_PerWorker(t *testing.T) {
	a := defaultWorkerInstallPrefix(true, "alice")
	b := defaultWorkerInstallPrefix(true, "bob")
	if a == b {
		t.Fatalf("alice + bob got same prefix %q", a)
	}
	if !strings.HasSuffix(a, "/workers/alice") {
		t.Errorf("alice prefix = %q", a)
	}
	if !strings.HasSuffix(b, "/workers/bob") {
		t.Errorf("bob prefix = %q", b)
	}
}

// v2.4-D-F4 X1 fix: install center default tcp-listen + helper that
// rewrites 0.0.0.0 to the host's name for the Modal install command.
func TestEnrollBootstrapHost(t *testing.T) {
	cases := []struct {
		in       string
		wantPort string
		wantHost bool // host expected non-empty
	}{
		{in: "", wantPort: ""},
		{in: "0.0.0.0:7300", wantPort: "7300", wantHost: true},
		{in: ":7300", wantPort: "7300", wantHost: true},
		{in: "127.0.0.1:7300", wantPort: "7300", wantHost: true},
		{in: "host.local:7300", wantPort: "7300", wantHost: true},
	}
	for _, c := range cases {
		got := enrollBootstrapHost(c.in)
		if c.in == "" {
			if got != "" {
				t.Errorf("empty in → %q, want \"\"", got)
			}
			continue
		}
		if !strings.HasSuffix(got, ":"+c.wantPort) {
			t.Errorf("in=%q got=%q want suffix :%s", c.in, got, c.wantPort)
		}
		if c.wantHost && len(got) <= len(":"+c.wantPort) {
			t.Errorf("in=%q got=%q missing host part", c.in, got)
		}
	}
}
