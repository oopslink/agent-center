package workerdaemon

import (
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/config"
)

// v2.7 FINDING-Q (#205): the worker's own state (long-term token + per-agent
// homes) must NOT land in the system /var/lib default when no config file was
// provided/discovered — that dir is unwritable in the #199 user-mode foreground
// run (`mkdir /var/lib/agent-center: permission denied`). It must derive from the
// loaded config when present, else a user-writable HOME-based, worker-id-keyed dir.
func TestWorkerStateDir_FindingQ(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sysDefault := config.Config{}
	sysDefault.Server.SqlitePath = "/var/lib/agent-center/agent-center.db"

	installed := config.Config{}
	installed.Server.SqlitePath = filepath.Join(home, ".agent-center", "workers", "w1", "var", "worker.db")

	t.Run("config file loaded → next to its sqlite data dir (unchanged)", func(t *testing.T) {
		got := workerStateDir(installed, "/some/etc/config.yaml", "w1")
		want := filepath.Join(home, ".agent-center", "workers", "w1", "var")
		if got != want {
			t.Fatalf("installed worker state dir = %q, want %q", got, want)
		}
	})

	t.Run("no config file → user-writable HOME dir, NOT /var/lib", func(t *testing.T) {
		got := workerStateDir(sysDefault, "", "w1")
		want := filepath.Join(home, ".agent-center", "workers", "w1", "var")
		if got != want {
			t.Fatalf("bare-run state dir = %q, want %q", got, want)
		}
		// The whole point of #205: never the unwritable system path.
		if filepath.Dir(workerTokenFilePath(sysDefault, "", "w1")) == "/var/lib/agent-center" {
			t.Fatalf("token must not persist under /var/lib in user-mode bare run")
		}
	})

	t.Run("token + agent-home both under the resolved state dir", func(t *testing.T) {
		dir := filepath.Join(home, ".agent-center", "workers", "w1", "var")
		if got, want := workerTokenFilePath(sysDefault, "", "w1"), filepath.Join(dir, "worker-token"); got != want {
			t.Fatalf("token path = %q, want %q", got, want)
		}
		if got, want := agentHomeBase(sysDefault, "", "w1"), filepath.Join(dir, "agent-homes"); got != want {
			t.Fatalf("agent-home base = %q, want %q", got, want)
		}
	})

	t.Run("config file but empty sqlite_path → user-writable HOME (not cwd)", func(t *testing.T) {
		// A config without sqlite_path can't anchor to an install data dir; the
		// HOME-based worker-id dir is still safer than scattering state in cwd.
		empty := config.Config{}
		want := filepath.Join(home, ".agent-center", "workers", "w1", "var")
		if got := workerStateDir(empty, "/etc/config.yaml", "w1"); got != want {
			t.Fatalf("config + empty sqlite_path → want HOME dir %q, got %q", want, got)
		}
	})
}

// When neither a config sqlite_path NOR a resolvable HOME exists, the worker uses
// cwd-relative names — never the unwritable system /var/lib default.
func TestWorkerStateDir_NoHomeLastResort(t *testing.T) {
	t.Setenv("HOME", "")
	sysDefault := config.Config{}
	sysDefault.Server.SqlitePath = "/var/lib/agent-center/agent-center.db"
	if got := workerStateDir(sysDefault, "", "w1"); got != "" {
		t.Fatalf("no config + no HOME → want empty, got %q", got)
	}
	if got := workerTokenFilePath(sysDefault, "", "w1"); got != "worker-token" {
		t.Fatalf("no config + no HOME → want cwd-relative worker-token, got %q", got)
	}
	if got := agentHomeBase(sysDefault, "", "w1"); got != "agent-homes" {
		t.Fatalf("no config + no HOME → want cwd-relative agent-homes, got %q", got)
	}
}

func TestSanitizeWorkerStateID(t *testing.T) {
	cases := map[string]string{
		"worker-1a2b3c4d": "worker-1a2b3c4d",
		"my_worker.01":    "my_worker.01",
		"a/b/c":           "a-b-c",
		"../etc/passwd":   "-etc-passwd", // separators → '-', leading dots trimmed; no '/' or '..' segment survives
		"":                "",
		"   ":             "",
	}
	for in, want := range cases {
		if got := sanitizeWorkerStateID(in); got != want {
			t.Errorf("sanitizeWorkerStateID(%q) = %q, want %q", in, got, want)
		}
	}
}
