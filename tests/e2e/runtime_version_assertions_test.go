package e2e

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workforce"
)

type runtimeBuildIdentity struct {
	Version string
	Commit  string
	Branch  string
	BuiltAt string
}

type runtimeCurrentArtifact struct {
	Build       runtimeBuildIdentity
	Link        string
	Target      string
	Binary      string
	BuildID     string
	ActivatedAt time.Time
}

func defaultRuntimeBuildIdentity() runtimeBuildIdentity {
	commit := gitOutput("rev-parse", "--short", "HEAD")
	if commit == "" {
		commit = "unknown"
	}
	return runtimeBuildIdentity{
		Version: "e2e-" + commit,
		Commit:  commit,
		Branch:  "e2e",
		BuiltAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func runtimeBuildIdentityFromEnv() runtimeBuildIdentity {
	return runtimeBuildIdentity{
		Version: strings.TrimSpace(os.Getenv("AC_E2E_EXPECTED_VERSION")),
		Commit:  strings.TrimSpace(os.Getenv("AC_E2E_EXPECTED_COMMIT")),
		Branch:  strings.TrimSpace(os.Getenv("AC_E2E_EXPECTED_BRANCH")),
		BuiltAt: strings.TrimSpace(os.Getenv("AC_E2E_EXPECTED_BUILT_AT")),
	}
}

func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

func buildAgentCenterBinary(out string, id runtimeBuildIdentity) error {
	ldflags := strings.Join([]string{
		"-X", "main.buildVersion=" + id.Version,
		"-X", "main.buildCommit=" + id.Commit,
		"-X", "main.buildBranch=" + id.Branch,
		"-X", "main.buildBuiltAt=" + id.BuiltAt,
	}, " ")
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", out, "github.com/oopslink/agent-center/cmd/agent-center")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build agent-center: %w", err)
	}
	return nil
}

type componentVersionReport struct {
	Component  string
	Expected   runtimeBuildIdentity
	Current    runtimeCurrentArtifact
	Running    runtimeBuildIdentity
	BuildID    string
	PID        int
	ParentPID  int
	StartedAt  string
	RuntimeFor string
	Adopted    bool
	Extra      string
}

func assertRuntimeVersionReports(reports []componentVersionReport) error {
	var errs []string
	for _, r := range reports {
		if r.Component == "" {
			r.Component = "unknown"
		}
		var mismatch []string
		if !sameBuild(r.Expected, r.Running) {
			mismatch = append(mismatch, fmt.Sprintf("expected=%s running=%s", formatBuild(r.Expected), formatBuild(r.Running)))
		}
		if r.Current.Build.Version != "" && !sameBuild(r.Expected, r.Current.Build) {
			mismatch = append(mismatch, fmt.Sprintf("expected=%s current=%s", formatBuild(r.Expected), formatBuild(r.Current.Build)))
		}
		if r.Current.BuildID != "" && r.BuildID != "" && r.Current.BuildID != r.BuildID {
			mismatch = append(mismatch, fmt.Sprintf("current_build_id=%s running_build_id=%s", r.Current.BuildID, r.BuildID))
		}
		if !r.Current.ActivatedAt.IsZero() && strings.TrimSpace(r.StartedAt) != "" {
			if started, ok := parseStartedAt(r.StartedAt); ok && started.Before(r.Current.ActivatedAt.Add(-2*time.Second)) {
				mismatch = append(mismatch, fmt.Sprintf("started_at=%s before_current_activated_at=%s",
					r.StartedAt, r.Current.ActivatedAt.Format(time.RFC3339Nano)))
			}
		}
		if len(mismatch) == 0 {
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"component=%s runtime_for=%s %s running_build_id=%s current_build_id=%s pid=%d parent_pid=%d started_at=%s adopted=%t current_link=%s current_target=%s current_binary=%s %s",
			r.Component, emptyDash(r.RuntimeFor), strings.Join(mismatch, " "),
			emptyDash(r.BuildID), emptyDash(r.Current.BuildID), r.PID, r.ParentPID,
			emptyDash(r.StartedAt), r.Adopted,
			emptyDash(r.Current.Link), emptyDash(r.Current.Target), emptyDash(r.Current.Binary), strings.TrimSpace(r.Extra),
		))
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New("runtime version assertion failed:\n" + strings.Join(errs, "\n"))
}

func sameBuild(a, b runtimeBuildIdentity) bool {
	return a.Version == b.Version &&
		a.Commit == b.Commit &&
		a.Branch == b.Branch &&
		a.BuiltAt == b.BuiltAt
}

func formatBuild(b runtimeBuildIdentity) string {
	return fmt.Sprintf("{version:%s commit:%s branch:%s built_at:%s}",
		emptyDash(b.Version), emptyDash(b.Commit), emptyDash(b.Branch), emptyDash(b.BuiltAt))
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func parseStartedAt(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "unknown" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func TestRuntimeVersionAssertions_ConsistentPasses(t *testing.T) {
	id := runtimeBuildIdentity{Version: "v-test", Commit: "abc123", Branch: "test", BuiltAt: "2026-08-06T00:00:00Z"}
	current := runtimeCurrentArtifact{
		Build: id, Link: "/tmp/ac/current", Target: "/tmp/ac/versions/v-test",
		Binary: "/tmp/ac/current/bin/agent-center", BuildID: "bid",
		ActivatedAt: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	err := assertRuntimeVersionReports([]componentVersionReport{
		{Component: "center", Expected: id, Current: current, Running: id, BuildID: "bid", PID: 101, StartedAt: "2026-08-06T00:00:01Z"},
		{Component: "worker", Expected: id, Current: current, Running: id, BuildID: "bid", PID: 102, StartedAt: "2026-08-06T00:00:02Z"},
		{Component: "agent-runtime", Expected: id, Current: current, Running: id, BuildID: "bid", PID: 103, ParentPID: 102, RuntimeFor: "agent-a"},
	})
	if err != nil {
		t.Fatalf("consistent runtime reports must pass: %v", err)
	}
}

func TestRuntimeVersionAssertions_AdoptedOldRuntimeSkewFailsWithDiagnostics(t *testing.T) {
	expected := runtimeBuildIdentity{Version: "v-new", Commit: "new123", Branch: "main", BuiltAt: "2026-08-06T00:01:00Z"}
	old := runtimeBuildIdentity{Version: "v-old", Commit: "old123", Branch: "main", BuiltAt: "2026-08-06T00:00:00Z"}
	current := runtimeCurrentArtifact{
		Build: expected, Link: "/tmp/ac/current", Target: "/tmp/ac/versions/v-new",
		Binary: "/tmp/ac/current/bin/agent-center", BuildID: "new-build-id",
		ActivatedAt: time.Date(2026, 8, 6, 0, 1, 0, 0, time.UTC),
	}
	err := assertRuntimeVersionReports([]componentVersionReport{{
		Component: "agent-runtime", Expected: expected, Current: current, Running: old, BuildID: "old-build-id",
		PID: 4242, ParentPID: 1, StartedAt: "2026-08-06T00:00:10Z",
		RuntimeFor: "agent-skew", Adopted: true, Extra: "runtime parent/adopt: worker restarted and adopted survivor",
	}})
	if err == nil {
		t.Fatal("old adopted runtime must fail")
	}
	msg := err.Error()
	for _, want := range []string{
		"component=agent-runtime", "expected={version:v-new", "running={version:v-old",
		"pid=4242", "parent_pid=1", "started_at=2026-08-06T00:00:10Z", "adopted=true",
		"running_build_id=old-build-id", "current_build_id=new-build-id", "current_target=/tmp/ac/versions/v-new",
		"runtime parent/adopt",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q:\n%s", want, msg)
		}
	}
}

func buildFromSystemVersion(body []byte) runtimeBuildIdentity {
	var doc struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Branch  string `json:"branch"`
		BuiltAt string `json:"built_at"`
	}
	_ = json.Unmarshal(body, &doc)
	return runtimeBuildIdentity{Version: doc.Version, Commit: doc.Commit, Branch: doc.Branch, BuiltAt: doc.BuiltAt}
}

func reportFromCenterVersion(expected runtimeBuildIdentity, current runtimeCurrentArtifact, body []byte) componentVersionReport {
	var doc struct {
		PID         int    `json:"pid"`
		ParentPID   int    `json:"parent_pid"`
		StartedAt   string `json:"started_at"`
		InstallPath string `json:"install_path"`
	}
	_ = json.Unmarshal(body, &doc)
	running := buildFromSystemVersion(body)
	return componentVersionReport{
		Component: "center",
		Expected:  expected,
		Current:   current,
		Running:   running,
		BuildID:   running.Version,
		PID:       doc.PID,
		ParentPID: doc.ParentPID,
		StartedAt: doc.StartedAt,
		Extra:     "install_path=" + doc.InstallPath,
	}
}

func reportFromWorkerInfo(expected runtimeBuildIdentity, current runtimeCurrentArtifact, info workforce.SystemInfo, pid int) componentVersionReport {
	return componentVersionReport{
		Component: "worker",
		Expected:  expected,
		Current:   current,
		Running: runtimeBuildIdentity{
			Version: strings.TrimSuffix(info.WorkerVersion, "+"+info.BuildCommit),
			Commit:  info.BuildCommit,
			Branch:  info.BuildBranch,
			BuiltAt: info.BuildBuiltAt,
		},
		BuildID:   info.AgentCenterVersion,
		PID:       firstInt(info.PID, pid),
		ParentPID: info.ParentPID,
		StartedAt: info.StartedAt,
		Extra:     "install_path=" + info.InstallPath,
	}
}

func reportFromAgentHealth(expected runtimeBuildIdentity, current runtimeCurrentArtifact, hr agentcontrol.HealthResponse, workerPID int) componentVersionReport {
	return componentVersionReport{
		Component: "agent-runtime",
		Expected:  expected,
		Current:   current,
		Running: runtimeBuildIdentity{
			Version: hr.Build.Version,
			Commit:  hr.Build.Commit,
			Branch:  hr.Build.Branch,
			BuiltAt: hr.Build.BuiltAt,
		},
		BuildID:    hr.Build.Version,
		PID:        hr.PID,
		ParentPID:  hr.ParentPID,
		StartedAt:  hr.StartedAt,
		RuntimeFor: hr.AgentID,
		Adopted:    workerPID > 0 && hr.ParentPID > 0 && hr.ParentPID != workerPID,
		Extra:      fmt.Sprintf("runtime parent/adopt: worker_pid=%d runtime_parent_pid=%d executable_path=%s", workerPID, hr.ParentPID, hr.ExecutablePath),
	}
}

func currentArtifactFromLayout(t *testing.T, installRoot string) runtimeCurrentArtifact {
	t.Helper()
	link := filepath.Join(installRoot, "current")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("stat current link %s: %v", link, err)
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("read current link %s: %v", link, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	bin := filepath.Join(link, "bin", "agent-center")
	id := runtimeBuildIdentity{
		Version: readMarker(t, link, "VERSION"),
		Commit:  readMarker(t, link, "COMMIT"),
		Branch:  readMarker(t, link, "BRANCH"),
		BuiltAt: readMarker(t, link, "BUILT_AT"),
	}
	return runtimeCurrentArtifact{Build: id, Link: link, Target: target, Binary: bin, BuildID: id.Version, ActivatedAt: info.ModTime().UTC()}
}

func readMarker(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s marker under %s: %v", name, dir, err)
	}
	return strings.TrimSpace(string(raw))
}

func firstInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func workerSockDirForTest(workerID string) string {
	sum := sha1.Sum([]byte(workerID))
	return filepath.Join(os.TempDir(), "acw-"+hex.EncodeToString(sum[:6]))
}

func waitAgentInfo(t *testing.T, sockDir, agentID string, within time.Duration) agentcontrol.HealthResponse {
	t.Helper()
	c := agentcontrol.NewClient(filepath.Join(sockDir, agentcontrol.SocketName(agentID)), time.Second)
	var lastErr error
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		hr, err := c.Info(context.Background())
		if err == nil && hr.AgentID == agentID {
			return hr
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("agent-runtime health for %s not ready within %s (last err=%v)", agentID, within, lastErr)
	return agentcontrol.HealthResponse{}
}

// waitAgentInfoMatchingPIDStore waits for a coherent snapshot across the
// runtime health socket and the worker's separately persisted PID store. The
// worker may replace a runtime between those two reads, so a one-shot mismatch
// is an observation race rather than proof that either source is corrupt.
func waitAgentInfoMatchingPIDStore(t *testing.T, sockDir, agentID string, within time.Duration) (agentcontrol.HealthResponse, int) {
	t.Helper()
	c := agentcontrol.NewClient(filepath.Join(sockDir, agentcontrol.SocketName(agentID)), time.Second)
	deadline := time.Now().Add(within)
	var lastInfo agentcontrol.HealthResponse
	var lastPID int
	var lastErr error
	for time.Now().Before(deadline) {
		hr, err := c.Info(context.Background())
		if err == nil && hr.AgentID == agentID {
			pid, pidErr := readAgentPID(sockDir, agentID)
			if pidErr == nil && pid > 0 && (hr.PID == 0 || hr.PID == pid) {
				return hr, pid
			}
			lastInfo, lastPID, lastErr = hr, pid, pidErr
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("agent-runtime health/PID store for %s did not converge within %s (health=%+v pidstore=%d last err=%v)",
		agentID, within, lastInfo, lastPID, lastErr)
	return agentcontrol.HealthResponse{}, 0
}

func waitFileWithContext(t *testing.T, path string, within time.Duration, context func() string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	extra := ""
	if context != nil {
		extra = context()
	}
	t.Fatalf("file %s never appeared within %s\n%s", path, within, extra)
}

func getSystemVersion(t *testing.T, webURL string) []byte {
	t.Helper()
	resp, err := http.Get(strings.TrimRight(webURL, "/") + "/api/system/version")
	if err != nil {
		t.Fatalf("GET /api/system/version: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/system/version status=%d", resp.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes()
}

func workerInfoFromAdmin(t *testing.T, sock, token, workerID string) workforce.SystemInfo {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		status, body := adminReq(t, sock, "GET", "/admin/workforce/worker/find-by-id?id="+workerID, token, nil)
		last = body
		if status == http.StatusOK {
			var doc struct {
				SystemInfo workforce.SystemInfo `json:"system_info"`
			}
			if err := json.Unmarshal([]byte(body), &doc); err == nil && !doc.SystemInfo.IsZero() {
				return doc.SystemInfo
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("worker %s did not report system_info; last body=%s", workerID, last)
	return workforce.SystemInfo{}
}

func workerInfoMatchingBuildFromAdmin(t *testing.T, sock, token, workerID string, expected runtimeBuildIdentity) workforce.SystemInfo {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last workforce.SystemInfo
	for time.Now().Before(deadline) {
		info := workerInfoFromAdmin(t, sock, token, workerID)
		last = info
		if info.BuildCommit == expected.Commit && info.BuildBranch == expected.Branch && info.BuildBuiltAt == expected.BuiltAt {
			return info
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("worker %s did not report expected build %s; last system_info=%+v", workerID, formatBuild(expected), last)
	return workforce.SystemInfo{}
}

func installLikeLayout(t *testing.T, root string, current runtimeBuildIdentity, bin string) string {
	t.Helper()
	versionDir := filepath.Join(root, "versions", current.Version)
	if err := os.MkdirAll(filepath.Join(versionDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(versionDir, "bin", "agent-center")
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "VERSION"), []byte(current.Version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "COMMIT"), []byte(current.Commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "BRANCH"), []byte(current.Branch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "BUILT_AT"), []byte(current.BuiltAt+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current")
	_ = os.Remove(link)
	if err := os.Symlink(versionDir, link); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(link, "bin", "agent-center")
}

func buildVariantBinary(t *testing.T, dir, name string, id runtimeBuildIdentity) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	if err := buildAgentCenterBinary(out, id); err != nil {
		t.Fatal(err)
	}
	return out
}

func parsePIDFile(t *testing.T, sockDir, agentID string) int {
	t.Helper()
	pid, err := readAgentPID(sockDir, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if pid <= 0 {
		t.Fatalf("agent-pids.json missing %s", agentID)
	}
	return pid
}

func readAgentPID(sockDir, agentID string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(sockDir, "agent-pids.json"))
	if err != nil {
		return 0, fmt.Errorf("read agent-pids.json: %w", err)
	}
	var pids map[string]int
	if err := json.Unmarshal(raw, &pids); err != nil {
		return 0, fmt.Errorf("decode agent-pids.json: %w", err)
	}
	return pids[agentID], nil
}

func envBool(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return false
	}
	ok, err := strconv.ParseBool(v)
	return err == nil && ok
}

func pickLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func shortRuntimeSandbox(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", pattern)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("runtime-version sandbox retained: %s", dir)
			return
		}
		_ = os.RemoveAll(dir)
	})
	return dir
}
