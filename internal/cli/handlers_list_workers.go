// handlers_list_workers.go — `agent-center list-local-workers` (v2.8 #170).
//
// Symmetric to `list-local-centers` (#211): lists every worker deployment
// installed on this machine — both production workers (under the prod install
// parent, e.g. ~/.agent-center.<x>) and test-instance workers (under
// ~/.agent-center-test/<id>/worker-<n>), each tagged with its `namespace`
// (prod|test) so a single discovery surface serves both operators and the
// test-instance tooling (#255). Discovery is filesystem-based: a directory
// qualifies when it holds an etc/config.yaml that is a WORKER config (a
// `worker:` section / worker.db sqlite path, never a center's web_console).
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oopslink/agent-center/internal/config"
	"gopkg.in/yaml.v3"
)

// ListLocalWorkersCommand surfaces all local worker deployments (v2.8 #170).
func ListLocalWorkersCommand() *Command {
	return &Command{
		Name:    "list-local-workers",
		Group:   "Admin",
		Summary: "List all worker deployments installed on this machine, prod + test (v2.8 #170)",
		Flags:   listLocalWorkersHandler,
	}
}

type localWorker struct {
	Namespace string `json:"namespace"` // "prod" | "test"
	Instance  string `json:"instance"`  // test-instance id (test only; "-" for prod)
	WorkerID  string `json:"worker_id"`
	Prefix    string `json:"prefix"`
	Bootstrap string `json:"bootstrap"`
	Mode      string `json:"mode"` // "service" | "foreground"
}

func listLocalWorkersHandler(fs *flag.FlagSet) Handler {
	userMode := fs.Bool("user-mode", isMacRuntime(), "scan user-mode install locations (default per OS)")
	outputF := fs.String("output", "text", "output format: text|json")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		workers, err := discoverLocalWorkers(*userMode)
		if err != nil {
			return PrintError(errw, FormatText, "list_workers_failed", err.Error(), ExitBusinessError)
		}
		if strings.ToLower(strings.TrimSpace(*outputF)) == "json" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(workers); err != nil {
				return PrintError(errw, FormatText, "list_workers_encode_failed", err.Error(), ExitBusinessError)
			}
			return ExitOK
		}
		if len(workers) == 0 {
			fmt.Fprintln(out, "No worker installs found on this machine.")
			return ExitOK
		}
		fmt.Fprintf(out, "%-5s %-10s %-22s %-40s %s\n", "NS", "INSTANCE", "WORKER-ID", "PREFIX", "MODE")
		for _, w := range workers {
			fmt.Fprintf(out, "%-5s %-10s %-22s %-40s %s\n", w.Namespace, w.Instance, w.WorkerID, w.Prefix, w.Mode)
		}
		return ExitOK
	}
}

// discoverLocalWorkers scans the prod install parent + the test-instance root
// for worker deployments.
func discoverLocalWorkers(userMode bool) ([]localWorker, error) {
	home, _ := os.UserHomeDir()
	sp, _ := platformPaths(runtimeOS(), userMode, home)
	var out []localWorker

	// --- prod: scan the install parent for <base>[.<instance>] worker dirs ---
	base := defaultInstallPrefix(userMode)
	parent := filepath.Dir(base)
	baseName := filepath.Base(base)
	if entries, err := os.ReadDir(parent); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if name != baseName && !strings.HasPrefix(name, baseName+".") {
				continue
			}
			prefix := filepath.Join(parent, name)
			if w, ok := readWorkerDeployment(prefix); ok {
				w.Namespace = "prod"
				w.Instance = "-"
				w.Mode = workerMode(sp, w.WorkerID)
				out = append(out, w)
			}
		}
	}

	// --- test: scan ~/.agent-center-test/<id>/worker-<n> ---
	if root, rerr := testInstanceRoot(); rerr == nil {
		if instEntries, err := os.ReadDir(root); err == nil {
			for _, ie := range instEntries {
				if !ie.IsDir() {
					continue
				}
				instID := ie.Name()
				instDir := filepath.Join(root, instID)
				wEntries, werr := os.ReadDir(instDir)
				if werr != nil {
					continue
				}
				for _, we := range wEntries {
					if !we.IsDir() || !strings.HasPrefix(we.Name(), "worker-") {
						continue
					}
					prefix := filepath.Join(instDir, we.Name())
					if w, ok := readWorkerDeployment(prefix); ok {
						w.Namespace = "test"
						w.Instance = instID
						w.Mode = workerMode(sp, w.WorkerID)
						out = append(out, w)
					}
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Instance != out[j].Instance {
			return out[i].Instance < out[j].Instance
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out, nil
}

// readWorkerDeployment returns a localWorker if prefix holds a worker config.
func readWorkerDeployment(prefix string) (localWorker, bool) {
	cfgPath := filepath.Join(prefix, "etc", "config.yaml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return localWorker{}, false
	}
	if !rawConfigIsWorker(raw) {
		return localWorker{}, false
	}
	w := localWorker{Prefix: prefix, WorkerID: "-", Bootstrap: "-"}
	if cfg, lerr := config.Load(config.LoadOptions{Path: cfgPath}); lerr == nil {
		if id := strings.TrimSpace(cfg.Worker.WorkerID); id != "" {
			w.WorkerID = id
		}
		if b := strings.TrimSpace(cfg.Worker.Bootstrap); b != "" {
			w.Bootstrap = b
		}
	}
	return w, true
}

// rawConfigIsWorker reports whether a config.yaml is a WORKER config — the
// inverse of rawConfigIsCenter. A worker declares a `worker:` section (#249)
// and/or a worker.db sqlite path, and never a web_console section (the center
// marker). We inspect the RAW yaml because config.Load backfills defaults that
// would mask the distinction.
func rawConfigIsWorker(raw []byte) bool {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return false
	}
	if _, ok := m["web_console"]; ok {
		return false // it's a center
	}
	if _, ok := m["worker"]; ok {
		return true
	}
	if srv, ok := m["server"].(map[string]any); ok {
		if sp, ok := srv["sqlite_path"].(string); ok && strings.HasSuffix(sp, "worker.db") {
			return true
		}
	}
	return false
}

// workerMode reports "service" (worker unit file present) or "foreground".
func workerMode(sp servicePaths, workerID string) string {
	wsp := applyWorkerIDToServicePaths(sp, workerID)
	if unitFileExists(wsp.WorkerUnitPath) {
		return "service"
	}
	return "foreground"
}
