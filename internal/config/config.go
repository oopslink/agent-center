// Package config loads agent-center configuration per 04-configuration §
// 1-5: YAML file → env override → CLI flag override → fail-fast validate.
//
// Unknown YAML fields and unknown env keys are rejected (conventions § 17:
// "未知协议字段当 noop 不上报" is forbidden).
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the in-memory representation of agent-center.yaml.
//
// Phase 1 surface: only fields that the server / migrate mode actually use
// are present. supervisor / worker / bridge / observability subtree fields
// listed in 04-configuration § 7 are intentionally absent — they'll grow
// when Phase 2+ needs them (04 § 5: 加字段有 default 时可直接 PR).
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Notification NotificationConfig `yaml:"notification"`
	Identity     IdentityConfig     `yaml:"identity"`
	Execution    ExecutionConfig    `yaml:"execution"`
	BlobStore    BlobStoreConfig    `yaml:"blob_store"`
	Peek         PeekConfig         `yaml:"peek"`
	Bridge       BridgeConfig       `yaml:"bridge"`
}

// BridgeConfig holds vendor-specific bridge settings. Per
// 04-configuration § 7.5 — v1 has only the feishu subtree populated.
type BridgeConfig struct {
	Feishu FeishuBridgeConfig `yaml:"feishu"`
}

// FeishuBridgeConfig captures bridge.feishu.* fields. Per conventions § 13:
// secrets MUST live in env (AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET) or a
// secret_file (`app_secret_file`) — plaintext `app_secret` in YAML is
// rejected fail-fast (Load returns ConfigError).
type FeishuBridgeConfig struct {
	Enabled       bool   `yaml:"enabled"`
	AppID         string `yaml:"app_id"`
	AppSecretFile string `yaml:"app_secret_file"`
	// AppSecret is resolved at load time from AppSecretFile or env; NOT
	// serialised from YAML directly. yaml tag "-" prevents accidental
	// disk writes.
	AppSecret string `yaml:"-"`
	// BaseURL overrides the open.feishu.cn endpoint (test injection).
	BaseURL string `yaml:"base_url"`
}

// BlobStoreConfig: 04-configuration / 01-blob-store. v1 LocalDir only.
type BlobStoreConfig struct {
	Kind string `yaml:"kind"` // "local" (default) | "s3" (future)
	Root string `yaml:"root"`
}

// PeekConfig holds peek-trace transport settings (Phase 4 § 3.7).
type PeekConfig struct {
	// WorkerSocket is the unix socket path the worker daemon serves the
	// peek-trace RPC on. Empty → "/var/run/agent-center-worker/peek.sock".
	WorkerSocket string `yaml:"worker_socket"`
}

// ServerConfig: 04-configuration § 7.1.
type ServerConfig struct {
	ListenAddr      string `yaml:"listen_addr"`
	SqlitePath      string `yaml:"sqlite_path"`
	AdminSocketPath string `yaml:"admin_socket_path"`
}

// NotificationConfig: 04-configuration § 7.4.
type NotificationConfig struct {
	DefaultChannel string `yaml:"default_channel"`
}

// ExecutionConfig: 04-configuration § 7.6.
type ExecutionConfig struct {
	SubmittedTimeoutSeconds      int `yaml:"submitted_timeout_seconds"`
	DefaultTimeoutHours          int `yaml:"default_timeout_hours"`
	DispatchAckTimeoutSeconds    int `yaml:"dispatch_ack_timeout_seconds"`
	InputRequestPingHours        int `yaml:"input_request_ping_hours"`
	InputRequestTimeoutHours     int `yaml:"input_request_timeout_hours"`
	ShimHelloTimeoutSeconds      int `yaml:"shim_hello_timeout_seconds"`
	ShimGoodbyeAckTimeoutHours   int `yaml:"shim_goodbye_ack_timeout_hours"`
	MaxExecutionsPerTask         int `yaml:"max_executions_per_task"`
	KillGraceSeconds             int `yaml:"kill_grace_seconds"`
}

// DispatchAckTimeout returns the Duration form (helper for clients).
func (e ExecutionConfig) DispatchAckTimeout() time.Duration {
	if e.DispatchAckTimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(e.DispatchAckTimeoutSeconds) * time.Second
}

// SubmittedTimeout returns the Duration form.
func (e ExecutionConfig) SubmittedTimeout() time.Duration {
	if e.SubmittedTimeoutSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(e.SubmittedTimeoutSeconds) * time.Second
}

// ExecutionTimeout returns the Duration form.
func (e ExecutionConfig) ExecutionTimeout() time.Duration {
	if e.DefaultTimeoutHours <= 0 {
		return 6 * time.Hour
	}
	return time.Duration(e.DefaultTimeoutHours) * time.Hour
}

// InputRequestPing returns the Duration form.
func (e ExecutionConfig) InputRequestPing() time.Duration {
	if e.InputRequestPingHours <= 0 {
		return 4 * time.Hour
	}
	return time.Duration(e.InputRequestPingHours) * time.Hour
}

// InputRequestTimeout returns the Duration form.
func (e ExecutionConfig) InputRequestTimeout() time.Duration {
	if e.InputRequestTimeoutHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(e.InputRequestTimeoutHours) * time.Hour
}

// ShimHelloTimeout returns the Duration form.
func (e ExecutionConfig) ShimHelloTimeout() time.Duration {
	if e.ShimHelloTimeoutSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(e.ShimHelloTimeoutSeconds) * time.Second
}

// KillGrace returns the Duration form.
func (e ExecutionConfig) KillGrace() time.Duration {
	if e.KillGraceSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(e.KillGraceSeconds) * time.Second
}

// IdentityConfig captures the v1 single-user default actor written into CLI
// emitted events. Not in 04-configuration § 7 yet — we add it here as a
// Phase 1 v1 simplification per plan § 6 R4 / R5 (Identity AR is Phase 5).
type IdentityConfig struct {
	DefaultUser string `yaml:"default_user"`
}

// DefaultConfig returns a Config seeded with the defaults from 04 § 7.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			ListenAddr:      ":7000",
			SqlitePath:      "/var/lib/agent-center/agent-center.db",
			AdminSocketPath: "/run/agent-center/admin.sock",
		},
		Notification: NotificationConfig{
			DefaultChannel: "",
		},
		Identity: IdentityConfig{
			DefaultUser: "hayang",
		},
		Execution: ExecutionConfig{
			SubmittedTimeoutSeconds:    300,
			DefaultTimeoutHours:        6,
			DispatchAckTimeoutSeconds:  30,
			InputRequestPingHours:      4,
			InputRequestTimeoutHours:   24,
			ShimHelloTimeoutSeconds:    60,
			ShimGoodbyeAckTimeoutHours: 24,
			MaxExecutionsPerTask:       3,
			KillGraceSeconds:           5,
		},
		BlobStore: BlobStoreConfig{
			Kind: "local",
			Root: "/var/lib/agent-center/blobs",
		},
		Peek: PeekConfig{
			WorkerSocket: "/var/run/agent-center-worker/peek.sock",
		},
		Bridge: BridgeConfig{
			Feishu: FeishuBridgeConfig{
				Enabled: false,
			},
		},
	}
}

// LoadOptions controls how config is loaded. Tests inject env vars and flag
// overrides via this struct.
type LoadOptions struct {
	// Path to YAML config file. Empty = no file (use defaults + env + flags).
	Path string
	// Env is the env-var lookup; defaults to os.LookupEnv when nil.
	Env func(key string) (string, bool)
	// FlagOverrides applied last (highest priority). Keys are YAML paths
	// joined with '.' (e.g. "server.listen_addr").
	FlagOverrides map[string]string
}

// Load returns a fully-validated Config or a ConfigError describing all
// failures. Per 04 § 4: any malformed input → exit 2 + complete diagnostics
// to stderr (we surface the errors; main.go is responsible for the exit
// code).
func Load(opts LoadOptions) (Config, error) {
	cfg := DefaultConfig()

	// Step 1: parse YAML if path given.
	if opts.Path != "" {
		raw, err := os.ReadFile(opts.Path)
		if err != nil {
			return Config{}, &ConfigError{Reasons: []string{
				fmt.Sprintf("read config file %q: %v", opts.Path, err),
			}}
		}
		// Step 1a: detect unknown YAML fields BEFORE strict decode (better
		// "did you mean" diagnostics).
		if err := checkUnknownYAMLKeys(raw, cfg); err != nil {
			return Config{}, err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, &ConfigError{Reasons: []string{
				fmt.Sprintf("parse YAML %q: %v", opts.Path, err),
			}}
		}
	}

	// Step 2: env var override.
	envFn := opts.Env
	if envFn == nil {
		envFn = os.LookupEnv
	}
	if err := applyEnvOverrides(&cfg, envFn); err != nil {
		return Config{}, err
	}

	// Step 3: flag override.
	if err := applyFlagOverrides(&cfg, opts.FlagOverrides); err != nil {
		return Config{}, err
	}

	// Step 4: validate.
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ConfigError aggregates all reasons for a load failure. Implements error.
type ConfigError struct {
	Reasons []string
}

// Error returns a multi-line diagnostic.
func (e *ConfigError) Error() string {
	if len(e.Reasons) == 0 {
		return "config: unknown failure"
	}
	var b strings.Builder
	b.WriteString("config error:\n")
	for _, r := range e.Reasons {
		b.WriteString("  - ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// checkUnknownYAMLKeys walks the YAML AST and reports any keys not present
// in the schema (Config struct + nested structs). 04 § 4 requires "did you
// mean" hints.
func checkUnknownYAMLKeys(raw []byte, schema Config) error {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return &ConfigError{Reasons: []string{fmt.Sprintf("parse YAML: %v", err)}}
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return &ConfigError{Reasons: []string{"top-level YAML must be a mapping"}}
	}
	known := collectKnownKeys(schema)
	var unknown []string
	walkYAMLKeys(root, "", known, &unknown)
	if len(unknown) > 0 {
		sort.Strings(unknown)
		reasons := make([]string, len(unknown))
		flat := flattenKnownKeys(known)
		for i, path := range unknown {
			reasons[i] = fmt.Sprintf("unknown YAML key %q%s",
				path, didYouMean(path, flat))
		}
		return &ConfigError{Reasons: reasons}
	}
	return nil
}

// keyTree mirrors the schema as a tree of known YAML key sets.
type keyTree map[string]keyTree

func collectKnownKeys(cfg Config) keyTree {
	// Manually maintained — keeps the schema definition centralized and
	// avoids reflection in a security-sensitive layer (config validation).
	return keyTree{
		"server": keyTree{
			"listen_addr":       nil,
			"sqlite_path":       nil,
			"admin_socket_path": nil,
		},
		"notification": keyTree{
			"default_channel": nil,
		},
		"identity": keyTree{
			"default_user": nil,
		},
		"blob_store": keyTree{
			"kind": nil,
			"root": nil,
		},
		"peek": keyTree{
			"worker_socket": nil,
		},
		"execution": keyTree{
			"submitted_timeout_seconds":     nil,
			"default_timeout_hours":         nil,
			"dispatch_ack_timeout_seconds":  nil,
			"input_request_ping_hours":      nil,
			"input_request_timeout_hours":   nil,
			"shim_hello_timeout_seconds":    nil,
			"shim_goodbye_ack_timeout_hours": nil,
			"max_executions_per_task":       nil,
			"kill_grace_seconds":            nil,
		},
		"bridge": keyTree{
			"feishu": keyTree{
				"enabled":         nil,
				"app_id":          nil,
				"app_secret_file": nil,
				"base_url":        nil,
			},
		},
	}
}

func walkYAMLKeys(n *yaml.Node, prefix string, known keyTree, unknown *[]string) {
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		child, ok := known[k]
		if !ok {
			*unknown = append(*unknown, path)
			continue
		}
		// child==nil means leaf — value should not itself be a mapping
		// with unknown subkeys (we don't enforce here; YAML decoder will
		// error if leaf value type mismatches).
		if child != nil {
			walkYAMLKeys(n.Content[i+1], path, child, unknown)
		}
	}
}

func flattenKnownKeys(t keyTree) []string {
	var out []string
	var walk func(node keyTree, prefix string)
	walk = func(node keyTree, prefix string) {
		for k, child := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if child == nil {
				out = append(out, path)
			} else {
				walk(child, path)
			}
		}
	}
	walk(t, "")
	sort.Strings(out)
	return out
}

func didYouMean(path string, candidates []string) string {
	best := ""
	bestDist := 1 << 30
	for _, c := range candidates {
		d := levenshtein(path, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if best == "" {
		return ""
	}
	// Only suggest when reasonably close.
	if bestDist > len(path)/2+2 {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	cur := make([]int, lb+1)
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// applyEnvOverrides reads AGENT_CENTER_* env vars and writes them into cfg.
// Per 04 § 2.1: prefix + path with '.' replaced by '_', uppercased.
func applyEnvOverrides(cfg *Config, env func(string) (string, bool)) error {
	type binding struct {
		envKey string
		setter func(string) error
	}
	bindings := []binding{
		{"AGENT_CENTER_SERVER_LISTEN_ADDR", func(v string) error {
			cfg.Server.ListenAddr = v
			return nil
		}},
		{"AGENT_CENTER_SERVER_SQLITE_PATH", func(v string) error {
			cfg.Server.SqlitePath = v
			return nil
		}},
		{"AGENT_CENTER_SERVER_ADMIN_SOCKET_PATH", func(v string) error {
			cfg.Server.AdminSocketPath = v
			return nil
		}},
		{"AGENT_CENTER_NOTIFICATION_DEFAULT_CHANNEL", func(v string) error {
			cfg.Notification.DefaultChannel = v
			return nil
		}},
		{"AGENT_CENTER_IDENTITY_DEFAULT_USER", func(v string) error {
			cfg.Identity.DefaultUser = v
			return nil
		}},
		{"AGENT_CENTER_BRIDGE_FEISHU_ENABLED", func(v string) error {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "on":
				cfg.Bridge.Feishu.Enabled = true
			case "0", "false", "no", "off", "":
				cfg.Bridge.Feishu.Enabled = false
			default:
				return fmt.Errorf("boolean: %q", v)
			}
			return nil
		}},
		{"AGENT_CENTER_BRIDGE_FEISHU_APP_ID", func(v string) error {
			cfg.Bridge.Feishu.AppID = v
			return nil
		}},
		{"AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET", func(v string) error {
			cfg.Bridge.Feishu.AppSecret = v
			return nil
		}},
		{"AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET_FILE", func(v string) error {
			cfg.Bridge.Feishu.AppSecretFile = v
			return nil
		}},
		{"AGENT_CENTER_BRIDGE_FEISHU_BASE_URL", func(v string) error {
			cfg.Bridge.Feishu.BaseURL = v
			return nil
		}},
	}
	var errs []string
	for _, b := range bindings {
		v, ok := env(b.envKey)
		if !ok {
			continue
		}
		if err := b.setter(v); err != nil {
			errs = append(errs, fmt.Sprintf("env %s: %v", b.envKey, err))
		}
	}
	if len(errs) > 0 {
		return &ConfigError{Reasons: errs}
	}
	return nil
}

// applyFlagOverrides applies the named flag values. Per 04 § 2.2: only a
// small subset is reachable via flag.
func applyFlagOverrides(cfg *Config, flags map[string]string) error {
	if len(flags) == 0 {
		return nil
	}
	allowed := map[string]func(string) error{
		"server.listen_addr": func(v string) error {
			cfg.Server.ListenAddr = v
			return nil
		},
		"server.sqlite_path": func(v string) error {
			cfg.Server.SqlitePath = v
			return nil
		},
		"identity.default_user": func(v string) error {
			cfg.Identity.DefaultUser = v
			return nil
		},
	}
	var errs []string
	for k, v := range flags {
		setter, ok := allowed[k]
		if !ok {
			errs = append(errs, fmt.Sprintf("flag override key %q not allowed", k))
			continue
		}
		if err := setter(v); err != nil {
			errs = append(errs, fmt.Sprintf("flag %s: %v", k, err))
		}
	}
	if len(errs) > 0 {
		return &ConfigError{Reasons: errs}
	}
	return nil
}

func validate(cfg *Config) error {
	var errs []string
	if strings.TrimSpace(cfg.Server.ListenAddr) == "" {
		errs = append(errs, "server.listen_addr: required")
	}
	if strings.TrimSpace(cfg.Server.SqlitePath) == "" {
		errs = append(errs, "server.sqlite_path: required")
	}
	if strings.TrimSpace(cfg.Identity.DefaultUser) == "" {
		errs = append(errs, "identity.default_user: required")
	}
	if cfg.Identity.DefaultUser != "" && strings.ContainsAny(cfg.Identity.DefaultUser, " :") {
		errs = append(errs, fmt.Sprintf("identity.default_user %q: must not contain space or ':'", cfg.Identity.DefaultUser))
	}
	if cfg.Bridge.Feishu.Enabled {
		if strings.TrimSpace(cfg.Bridge.Feishu.AppID) == "" {
			errs = append(errs, "bridge.feishu.app_id: required when bridge.feishu.enabled=true")
		}
		// Resolve the secret. Preference order:
		//   1. env AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET (already applied)
		//   2. app_secret_file
		// Plaintext "app_secret:" in YAML is intentionally not a recognised
		// key (would be rejected by the unknown-keys walk).
		if cfg.Bridge.Feishu.AppSecret == "" && cfg.Bridge.Feishu.AppSecretFile != "" {
			b, err := os.ReadFile(cfg.Bridge.Feishu.AppSecretFile)
			if err != nil {
				errs = append(errs,
					fmt.Sprintf("bridge.feishu.app_secret_file %q: %v",
						cfg.Bridge.Feishu.AppSecretFile, err))
			} else {
				cfg.Bridge.Feishu.AppSecret = strings.TrimSpace(string(b))
			}
		}
		if cfg.Bridge.Feishu.AppSecret == "" {
			errs = append(errs,
				"bridge.feishu: app_secret required via env AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET or bridge.feishu.app_secret_file (conventions § 13: no plaintext in YAML)")
		}
	}
	if len(errs) > 0 {
		return &ConfigError{Reasons: errs}
	}
	return nil
}

// ParsePort is a small helper used by callers that need numeric ports —
// returns -1 on failure.
func ParsePort(addr string) int {
	_, port, ok := strings.Cut(addr, ":")
	if !ok {
		return -1
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return -1
	}
	return n
}

// AsErrorList lets callers extract the per-line diagnostics for stderr.
func AsErrorList(err error) []string {
	var ce *ConfigError
	if errors.As(err, &ce) {
		return ce.Reasons
	}
	if err != nil {
		return []string{err.Error()}
	}
	return nil
}
