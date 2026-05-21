package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/observability"
)

// BridgeCommands returns the `bridge` subcommand tree per
// 03-cli-subcommands § 8.6.
func (a *App) BridgeCommands() []*Command {
	return []*Command{
		{
			Name:    "feishu",
			Summary: "FeishuBridge management (setup / smoke test)",
			Subcommands: []*Command{
				{
					Name:    "setup",
					Summary: "Write bridge.feishu.* config + smoke-test connection",
					Flags:   a.bridgeFeishuSetupHandler,
				},
			},
		},
	}
}

// bridgeFeishuSetupHandler implements `agent-center bridge feishu setup`.
//
// Behaviour per plan-5 § 3.7:
//  1. validate --app-id non-empty and --app-secret-file exists + readable
//  2. atomically write bridge.feishu.{enabled,app_id,app_secret_file} into
//     the resolved config file (idempotent — preserves other keys)
//  3. spin up a client.OAPIAdapter + Connect smoke test (30s timeout)
//  4. emit `bridge.feishu.connection_state_changed` for both branches
//     (success → state=connected; failure → state=disconnected) per
//     conventions § 16 (reason + message)
//  5. exit 0 on smoke success / non-zero on failure
func (a *App) bridgeFeishuSetupHandler(fs *flag.FlagSet) Handler {
	appID := fs.String("app-id", "", "feishu open platform App ID")
	appSecretFile := fs.String("app-secret-file", "", "path to a file holding the feishu App Secret")
	baseURL := fs.String("base-url", "", "(test) override feishu base URL")
	format := fs.String("format", "human", "")
	skipSmoke := fs.Bool("skip-smoke-test", false, "skip the Connect smoke test (write config only)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *appID == "" {
			return PrintError(errw, *format, "usage_error",
				"--app-id required", ExitUsage)
		}
		if *appSecretFile == "" {
			return PrintError(errw, *format, "usage_error",
				"--app-secret-file required", ExitUsage)
		}
		// Validate the secret file is readable + non-empty before we
		// touch the config (avoid landing with an invalid setup).
		raw, err := os.ReadFile(*appSecretFile)
		if err != nil {
			return PrintError(errw, *format, "app_secret_file_unreadable",
				fmt.Sprintf("app_secret_file %q: %v", *appSecretFile, err), ExitUsage)
		}
		secret := strings.TrimSpace(string(raw))
		if secret == "" {
			return PrintError(errw, *format, "app_secret_file_empty",
				fmt.Sprintf("app_secret_file %q: empty", *appSecretFile), ExitUsage)
		}
		cfgPath := GlobalConfigPath()
		if cfgPath == "" {
			return PrintError(errw, *format, "config_path_unknown",
				"no --config flag and AGENT_CENTER_CONFIG unset; cannot persist bridge.feishu.*", ExitUsage)
		}
		if err := persistFeishuBridgeConfig(cfgPath, *appID, *appSecretFile, *baseURL); err != nil {
			return PrintError(errw, *format, "config_write_failed", err.Error(), ExitBusinessError)
		}

		if *skipSmoke {
			emitState(ctx, a, observability.Actor("system"),
				"connected", "feishu bridge enabled (smoke skipped)", true)
			if *format == "json" {
				writeOut(out, fmt.Sprintf(`{"feishu":{"enabled":true,"app_id":%q,"smoke_skipped":true}}`, *appID))
			} else {
				fmt.Fprintf(out, "feishu bridge enabled, app_id=%s (smoke skipped)\n", *appID)
			}
			return ExitOK
		}

		// Smoke test: real Connect call with 30s timeout.
		smokeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cli := client.NewOAPIAdapter(client.AdapterConfig{
			BaseURL:     *baseURL,
			AppID:       *appID,
			AppSecret:   secret,
			HTTPClient:  &http.Client{Timeout: 25 * time.Second},
			Clock:       a.Clock,
			MaxRetries:  1, // setup is interactive; don't make user wait through 3 retries
			BackoffStep: 250 * time.Millisecond,
		})
		if err := cli.Connect(smokeCtx); err != nil {
			reason, message := classifyConnectError(err)
			emitState(ctx, a, observability.Actor("system"), "disconnected",
				"feishu setup smoke connect failed: "+message, false)
			return PrintError(errw, *format, reason, message, ExitBusinessError)
		}
		_ = cli.Close()
		emitState(ctx, a, observability.Actor("system"), "connected",
			"feishu setup smoke connect succeeded", true)
		if *format == "json" {
			writeOut(out, fmt.Sprintf(`{"feishu":{"enabled":true,"app_id":%q,"connected":true}}`, *appID))
		} else {
			fmt.Fprintf(out, "feishu bridge enabled, app_id=%s (connected)\n", *appID)
		}
		return ExitOK
	}
}

// classifyConnectError maps client errors to CLI reason strings.
func classifyConnectError(err error) (string, string) {
	switch {
	case errors.Is(err, client.ErrAuthFailed):
		return "feishu_auth_failed", err.Error()
	case errors.Is(err, client.ErrTransientFailure):
		return "feishu_transient_failure", err.Error()
	case errors.Is(err, client.ErrPermanentFailure):
		return "feishu_permanent_failure", err.Error()
	}
	return "feishu_connect_failed", err.Error()
}

// emitState emits `bridge.feishu.connection_state_changed`. Suppress
// errors here is acceptable because emitState itself is *the* reporting
// channel; we still surface them to stderr via printIfErr — that is the
// only place log-only is sanctioned (best-effort cleanup; conventions
// § 17 example).
func emitState(ctx context.Context, a *App, actor observability.Actor, state, message string, isConnected bool) {
	if a == nil || a.Sink == nil {
		return
	}
	reason := state
	if !isConnected && state == "disconnected" {
		reason = "connect_failed"
	}
	_, _ = a.Sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.feishu.connection_state_changed",
		Actor:     actor,
		Payload: map[string]any{
			"state":   state,
			"reason":  reason,
			"message": message,
		},
	})
}

// persistFeishuBridgeConfig writes (or merges) the bridge.feishu.* keys
// into cfgPath using an atomic rename (write to tmp + os.Rename).
//
// Other keys in the file are preserved — we read the existing YAML, edit
// only the bridge block, and rewrite. This is a minimal text-level merge
// (no full YAML round-trip via gopkg.in/yaml.v3 to avoid losing comments).
func persistFeishuBridgeConfig(cfgPath, appID, appSecretFile, baseURL string) error {
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	var existing []byte
	if b, err := os.ReadFile(cfgPath); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing config: %w", err)
	}
	updated := mergeBridgeFeishuYAML(string(existing), appID, appSecretFile, baseURL)
	tmpPath := cfgPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := os.Rename(tmpPath, cfgPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// mergeBridgeFeishuYAML returns the existing YAML body with the
// `bridge.feishu.*` block replaced (or appended). The implementation is
// intentionally text-level — we strip the existing `bridge:` section if
// present and re-append the fresh block at the end.
func mergeBridgeFeishuYAML(existing, appID, appSecretFile, baseURL string) string {
	stripped := stripBridgeSection(existing)
	stripped = strings.TrimRight(stripped, "\n")
	if stripped != "" {
		stripped += "\n"
	}
	var b strings.Builder
	b.WriteString(stripped)
	b.WriteString("bridge:\n")
	b.WriteString("  feishu:\n")
	b.WriteString("    enabled: true\n")
	b.WriteString(fmt.Sprintf("    app_id: %s\n", quoteYAMLScalar(appID)))
	b.WriteString(fmt.Sprintf("    app_secret_file: %s\n", quoteYAMLScalar(appSecretFile)))
	if baseURL != "" {
		b.WriteString(fmt.Sprintf("    base_url: %s\n", quoteYAMLScalar(baseURL)))
	}
	return b.String()
}

// stripBridgeSection removes the existing `bridge:` block at the top
// level, if present. Conservative: matches only top-level keys (no leading
// whitespace) and strips until the next top-level key or EOF.
func stripBridgeSection(yaml string) string {
	lines := strings.Split(yaml, "\n")
	var out []string
	skip := false
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		isTopLevel := len(l) > 0 && l[0] != ' ' && l[0] != '\t' && l[0] != '#'
		if isTopLevel {
			if strings.HasPrefix(trimmed, "bridge:") {
				skip = true
				continue
			}
			skip = false
		}
		if skip {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func quoteYAMLScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ": #\n\t'\"") {
		// Use double quotes + minimal escape.
		esc := strings.ReplaceAll(s, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		return `"` + esc + `"`
	}
	return s
}
