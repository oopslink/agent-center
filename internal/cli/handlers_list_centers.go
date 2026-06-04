// handlers_list_centers.go — `agent-center list-local-centers` (v2.7.1 #211).
//
// Lists every center deployment installed on this machine so an operator (or a
// Tester running multiple instances in parallel) can see, at a glance, which
// instances exist, where they live, their ports, whether they run as a managed
// service or foreground, and whether the web console is currently reachable.
// Discovery is filesystem-based: it scans the install parent dir for
// `<base>` (the default instance) + `<base>.<instance>` prefixes that contain an
// etc/config.yaml.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/config"
	"gopkg.in/yaml.v3"
)

// ListLocalCentersCommand surfaces all local center deployments (v2.7.1 #211).
func ListLocalCentersCommand() *Command {
	return &Command{
		Name:    "list-local-centers",
		Group:   "Admin",
		Summary: "List all agent-center deployments installed on this machine (v2.7.1 #211 multi-instance)",
		Flags:   listLocalCentersHandler,
	}
}

type localCenter struct {
	Instance   string
	Prefix     string
	WebPort    string
	ServerPort string
	AdminPort  string
	Mode       string // "service" | "foreground"
	Online     string // "yes" | "no" | "?"
}

func listLocalCentersHandler(fs *flag.FlagSet) Handler {
	userMode := fs.Bool("user-mode", isMacRuntime(), "scan user-mode install locations (default per OS)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		centers, err := discoverLocalCenters(*userMode)
		if err != nil {
			return PrintError(errw, FormatText, "list_centers_failed", err.Error(), ExitBusinessError)
		}
		if len(centers) == 0 {
			fmt.Fprintln(out, "No agent-center installs found on this machine.")
			return ExitOK
		}
		fmt.Fprintf(out, "%-16s %-34s %-6s %-7s %-6s %-11s %s\n",
			"INSTANCE", "PREFIX", "WEB", "SERVER", "ADMIN", "MODE", "ONLINE")
		for _, c := range centers {
			fmt.Fprintf(out, "%-16s %-34s %-6s %-7s %-6s %-11s %s\n",
				c.Instance, c.Prefix, c.WebPort, c.ServerPort, c.AdminPort, c.Mode, c.Online)
		}
		return ExitOK
	}
}

// discoverLocalCenters scans the install parent dir for center deployments. A
// directory qualifies when its name is the default base ("<base>") or a named
// instance ("<base>.<instance>") AND it contains etc/config.yaml.
func discoverLocalCenters(userMode bool) ([]localCenter, error) {
	base := defaultInstallPrefix(userMode) // e.g. ~/.agent-center or /opt/agent-center
	parent := filepath.Dir(base)
	baseName := filepath.Base(base)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	sp, _ := platformPaths(runtimeOS(), userMode, home)

	var out []localCenter
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name != baseName && !strings.HasPrefix(name, baseName+".") {
			continue
		}
		prefix := filepath.Join(parent, name)
		cfgPath := filepath.Join(prefix, "etc", "config.yaml")
		raw, rerr := os.ReadFile(cfgPath)
		if rerr != nil {
			continue // no config.yaml — not an install at all
		}
		// v2.7.1 #212: a sibling dir with a config.yaml isn't necessarily a CENTER —
		// `install worker --prefix ~/.agent-center.<x>` writes a worker config.yaml too
		// (no web_console section, sqlite worker.db). config.Load would backfill default
		// ports and list it as a bogus center row. Require the center marker (a
		// web_console section, which workers never write) so worker prefixes are excluded.
		if !rawConfigIsCenter(raw) {
			continue
		}
		lc := localCenter{Prefix: prefix, Instance: DefaultInstance, WebPort: "-", ServerPort: "-", AdminPort: "-"}
		// Derive the instance from the dir suffix as a fallback; the config's
		// instance field (when present + non-empty) is authoritative.
		if name != baseName {
			lc.Instance = strings.TrimPrefix(name, baseName+".")
		}
		var webAddr string
		if cfg, lerr := config.Load(config.LoadOptions{Path: cfgPath}); lerr == nil {
			if inst := strings.TrimSpace(cfg.Server.Instance); inst != "" {
				lc.Instance = inst
			}
			lc.WebPort = portOf(cfg.WebConsole.ListenAddr)
			lc.ServerPort = portOf(cfg.Server.ListenAddr)
			lc.AdminPort = portOf(cfg.Server.AdminTCPListen)
			webAddr = cfg.WebConsole.ListenAddr
		}
		// service vs foreground = whether THIS instance's center unit file exists.
		isp := applyInstanceToServicePaths(sp, lc.Instance)
		if unitFileExists(isp.CenterUnitPath) {
			lc.Mode = "service"
		} else {
			lc.Mode = "foreground"
		}
		lc.Online = centerOnline(webAddr)
		out = append(out, lc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out, nil
}

// rawConfigIsCenter reports whether a config.yaml is a CENTER config (vs a worker
// config). v2.7.1 #212: a center declares a web_console section (writeCenterConfig)
// + sqlite agent-center.db; a worker (writeWorkerConfig) writes neither (no
// web_console, sqlite worker.db). We inspect the RAW yaml — config.Load backfills
// web_console + ports with defaults, which would mask the difference and list a
// worker prefix as a bogus center row.
func rawConfigIsCenter(raw []byte) bool {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return false
	}
	if _, ok := m["web_console"]; ok {
		return true // the definitive center marker (workers never write it)
	}
	// Belt-and-suspenders for a hand-edited center config without web_console.
	if srv, ok := m["server"].(map[string]any); ok {
		if sp, ok := srv["sqlite_path"].(string); ok && strings.HasSuffix(sp, "agent-center.db") {
			return true
		}
	}
	return false
}

// portOf extracts the port from a "host:port" / ":port" listen address.
func portOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "-"
	}
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return addr // unparseable → show raw so the operator can see something
}

// centerOnline reports whether the web console port is currently accepting
// connections (a cheap liveness probe; "?" when the address is unknown).
func centerOnline(webAddr string) string {
	webAddr = strings.TrimSpace(webAddr)
	if webAddr == "" {
		return "?"
	}
	host, port, err := net.SplitHostPort(webAddr)
	if err != nil || port == "" {
		return "?"
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	conn, derr := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
	if derr != nil {
		return "no"
	}
	_ = conn.Close()
	return "yes"
}
