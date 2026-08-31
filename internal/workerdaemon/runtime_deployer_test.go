package workerdaemon

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	adminapi "github.com/oopslink/agent-center/internal/admin/api"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

func TestSourceRuntimeDeployerReportsAuthoritativeReadback(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	var commands []string
	d := newSourceRuntimeDeployer(root, "worker-1", func(string) {})
	d.readback = func(_ context.Context, mode, gotPrefix, _, gotSHA string) (runtimeBuildReadback, error) {
		if mode != "center" || gotPrefix != prefix {
			t.Fatalf("readback mode/prefix = %s/%s", mode, gotPrefix)
		}
		if gotSHA != sha {
			t.Fatalf("readback expected sha = %s, want %s", gotSHA, sha)
		}
		return runtimeBuildReadback{Version: "runtime-deploy-" + sha[:12], Commit: sha, Health: "healthy"}, nil
	}
	d.run = func(_ context.Context, dir, name string, args []string, _ []string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		switch {
		case name == "git" && len(args) > 0 && args[0] == "clone":
			if err := os.MkdirAll(runtimedeploy.ManagedSourceDir(root, sha), 0o700); err != nil {
				return nil, err
			}
		case name == "make" && slices.Contains(args, "OUT="+filepath.Join(root, "stage-"+sha[:12])):
			if err := os.MkdirAll(filepath.Join(root, "stage-"+sha[:12], "bin"), 0o700); err != nil {
				return nil, err
			}
		}
		return []byte("ok\n"), nil
	}

	got, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL:           "https://example.invalid/repo.git",
		TargetRef:         "refs/heads/main",
		TargetSHA:         sha,
		VerifiedTargetSHA: sha,
		VerifiedBaseSHA:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		VerifiedAt:        "2026-08-31T00:00:00Z",
		Mode:              "center",
		Prefix:            prefix,
	})
	if err != nil {
		t.Fatalf("DeployRestart: %v", err)
	}
	if got.TargetSHA != sha || got.RunningSHA != sha || got.RunningVersion != "runtime-deploy-"+sha[:12] ||
		got.RunningCommit != sha || !got.RestartTerminalSuccess || got.PostRestartHealthStatus != "healthy" {
		t.Fatalf("deploy result missing authoritative readback: %+v", got)
	}
	if !slices.Contains(commands, "make release-dir VERSION=runtime-deploy-"+sha[:12]+" COMMIT="+sha+" OUT="+filepath.Join(root, "stage-"+sha[:12])) {
		t.Fatalf("make release-dir did not receive full COMMIT; commands=%v", commands)
	}
	for _, command := range commands {
		if strings.Contains(command, filepath.Join(prefix, "current", "bin", "agent-center")+" version") {
			t.Fatalf("must not use installed artifact version as running readback; commands=%v", commands)
		}
	}
}

func TestSourceRuntimeDeployerRejectsInstalledArtifactMismatchReadback(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	installedSHA := strings.Repeat("b", 40)
	d := newSourceRuntimeDeployer(root, "worker-1", func(string) {})
	d.readback = func(context.Context, string, string, string, string) (runtimeBuildReadback, error) {
		return runtimeBuildReadback{Version: "runtime-deploy-" + installedSHA[:12], Commit: installedSHA, Health: "healthy"}, nil
	}
	d.run = func(_ context.Context, _ string, name string, args []string, _ []string) ([]byte, error) {
		switch {
		case name == "git" && len(args) > 0 && args[0] == "clone":
			return []byte("ok\n"), os.MkdirAll(runtimedeploy.ManagedSourceDir(root, sha), 0o700)
		default:
			return []byte("ok\n"), nil
		}
	}
	_, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL:           "https://example.invalid/repo.git",
		TargetRef:         "refs/heads/main",
		TargetSHA:         sha,
		VerifiedTargetSHA: sha,
		VerifiedBaseSHA:   strings.Repeat("b", 40),
		VerifiedAt:        "2026-08-31T00:00:00Z",
		Mode:              "center",
		Prefix:            prefix,
	})
	if err == nil || !strings.Contains(err.Error(), "running_sha "+installedSHA+" does not match verified target "+sha) {
		t.Fatalf("installed-artifact mismatch should fail closed, got %v", err)
	}
}

func TestReadCenterAdminHealthUsesRunningEndpointBuildIdentity(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	sock := filepath.Join("/tmp", "ac-rd-"+strings.ReplaceAll(t.Name(), "/", "-")+".sock")
	_ = os.Remove(sock)
	t.Cleanup(func() { _ = os.Remove(sock) })
	if err := os.MkdirAll(filepath.Join(prefix, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "etc", "config.yaml"), []byte("server:\n  sqlite_path: \""+filepath.Join(root, "center.db")+"\"\n  admin_socket_path: \""+sock+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	srv := adminapi.NewServerWithDeps(sock, adminapi.ServerDeps{
		Version: "runtime-deploy-" + sha[:12],
		Commit:  sha,
		Branch:  "test",
		BuiltAt: "2026-08-31T00:00:00Z",
	})
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()
	waitForRuntimeDeploySocket(t, sock, errCh)

	got, err := readCenterAdminHealth(context.Background(), prefix)
	if err != nil {
		t.Fatalf("readCenterAdminHealth: %v", err)
	}
	if got.Version != "runtime-deploy-"+sha[:12] || got.Commit != sha || got.Health != "healthy" {
		t.Fatalf("readback = %+v", got)
	}
}

func TestReadWorkerAdminReadbackUsesAuthenticatedWorkerProjection(t *testing.T) {
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	tokenPath := writeRuntimeReadbackToken(t, "acat_worker")
	var authz string
	sock, shutdown := serveRuntimeReadbackWorker(t, func(w http.ResponseWriter, r *http.Request) {
		authz = r.Header.Get("Authorization")
		if r.URL.Query().Get("id") != "worker-1" {
			t.Fatalf("worker id query = %q", r.URL.Query().Get("id"))
		}
		_, _ = w.Write([]byte(`{"worker_id":"worker-1","status":"online","version":7,"system_info":{"worker_version":"runtime-deploy-` + sha[:12] + `","build_commit":"` + sha + `","pid":123,"started_at":"2026-08-31T00:00:00Z"}}`))
	})
	defer shutdown()

	got, err := readWorkerAdminReadback(context.Background(), "unix:"+sock, "", tokenPath, "worker-1", sha)
	if err != nil {
		t.Fatalf("readWorkerAdminReadback: %v", err)
	}
	if authz != "Bearer acat_worker" {
		t.Fatalf("missing authenticated bearer, got %q", authz)
	}
	if got.Version != "runtime-deploy-"+sha[:12] || got.Commit != sha || got.Health != "healthy" {
		t.Fatalf("readback = %+v", got)
	}
}

func TestReadWorkerAdminReadbackFailClosedStates(t *testing.T) {
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	stale := strings.Repeat("b", 40)
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "stale sha",
			body: `{"worker_id":"worker-1","status":"online","system_info":{"worker_version":"runtime-deploy-` + stale[:12] + `","build_commit":"` + stale + `"}}`,
			want: "stale running sha",
		},
		{
			name: "unhealthy",
			body: `{"worker_id":"worker-1","status":"offline","system_info":{"worker_version":"runtime-deploy-` + sha[:12] + `","build_commit":"` + sha + `"}}`,
			want: "unhealthy status",
		},
		{
			name: "unavailable",
			body: `{"error":"worker_not_found"}`,
			want: "status=404",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokenPath := writeRuntimeReadbackToken(t, "acat_worker")
			sock, shutdown := serveRuntimeReadbackWorker(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "unavailable" {
					w.WriteHeader(http.StatusNotFound)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			defer shutdown()
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			_, err := readWorkerAdminReadback(ctx, "unix:"+sock, "", tokenPath, "worker-1", sha)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestReadWorkerAdminReadbackDisconnectedFailsClosed(t *testing.T) {
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	tokenPath := writeRuntimeReadbackToken(t, "acat_worker")
	sock := shortSock(t, "missing.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := readWorkerAdminReadback(ctx, "unix:"+sock, "", tokenPath, "worker-1", sha)
	if err == nil || !strings.Contains(err.Error(), "worker readback unavailable") {
		t.Fatalf("err=%v, want unavailable disconnect failure", err)
	}
}

func TestReadWorkerAdminReadbackTimeoutFailsClosed(t *testing.T) {
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	tokenPath := writeRuntimeReadbackToken(t, "acat_worker")
	sock, shutdown := serveRuntimeReadbackWorker(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"worker_id":"worker-1","status":"online","system_info":{"worker_version":"runtime-deploy-` + sha[:12] + `","build_commit":"` + sha + `"}}`))
	})
	defer shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := readWorkerAdminReadback(ctx, "unix:"+sock, "", tokenPath, "worker-1", sha)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err=%v, want timeout failure", err)
	}
}

func waitForRuntimeDeploySocket(t *testing.T, sock string, errCh <-chan error) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-errCh:
			t.Fatalf("admin server exited before socket ready: %v", err)
		case <-deadline:
			t.Fatalf("socket %s not ready", sock)
		default:
			if _, err := os.Stat(sock); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func writeRuntimeReadbackToken(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker-token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func serveRuntimeReadbackWorker(t *testing.T, h http.HandlerFunc) (string, func()) {
	t.Helper()
	sock := shortSock(t, "runtime-readback.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/workforce/worker/find-by-id", h)
	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	waitForRuntimeDeploySocket(t, sock, errCh)
	return sock, func() {
		_ = srv.Shutdown(context.Background())
		_ = ln.Close()
		_ = os.Remove(sock)
	}
}
