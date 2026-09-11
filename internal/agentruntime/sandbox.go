package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

const (
	SandboxProviderTartMacOSVM = "tart_macos_vm"

	SandboxStateProvisioning  = "provisioning"
	SandboxStateReady         = "ready"
	SandboxStateRunning       = "running"
	SandboxStateSuspended     = "suspended"
	SandboxStateDegraded      = "degraded"
	SandboxStateResetRequired = "reset_required"
	SandboxStateDeleted       = "deleted"
)

var ErrUnsupportedSandboxProvider = errors.New("agentruntime: unsupported sandbox provider")

type SandboxConfig struct {
	Enabled  bool
	Provider string
}

type SandboxEnsureRequest struct {
	AgentID  string
	WorkerID string
	HomeDir  string
	Config   SandboxConfig
}

type SandboxBinding struct {
	SandboxID           string            `json:"sandbox_id"`
	AgentID             string            `json:"agent_id"`
	WorkerID            string            `json:"worker_id"`
	Provider            string            `json:"provider"`
	VMName              string            `json:"vm_name"`
	State               string            `json:"state"`
	ComputerUseEndpoint string            `json:"computer_use_endpoint,omitempty"`
	ComputerUseEnv      map[string]string `json:"computer_use_env,omitempty"`
	BootstrapPath       string            `json:"bootstrap_path,omitempty"`
	ConsoleCommand      string            `json:"console_command,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	LastHealthAt        time.Time         `json:"last_health_at,omitempty"`
	LastError           string            `json:"last_error,omitempty"`
}

type tartVMInfo struct {
	Running bool   `json:"Running"`
	State   string `json:"State"`
}

func tartStateFromJSON(raw []byte) (string, bool) {
	var info tartVMInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return "", false
	}
	if info.Running {
		return SandboxStateRunning, true
	}
	switch strings.ToLower(strings.TrimSpace(info.State)) {
	case "running":
		return SandboxStateRunning, true
	case "suspended":
		return SandboxStateSuspended, true
	case "stopped":
		return SandboxStateReady, true
	default:
		return "", false
	}
}

func tartVMState(ctx context.Context, vmName string) (string, bool) {
	if strings.TrimSpace(vmName) == "" {
		return "", false
	}
	out, err := exec.CommandContext(ctx, "tart", "get", vmName, "--format", "json").CombinedOutput()
	if err != nil {
		return "", false
	}
	return tartStateFromJSON(out)
}

func tartNotRunningError(out string) bool {
	return strings.Contains(strings.ToLower(out), "is not running")
}

func tartRestoreFailed(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "failed to restore") || strings.Contains(lower, "vzerrordomain code=12")
}

func (b SandboxBinding) ComputerUseStatus() string {
	switch b.State {
	case SandboxStateRunning:
		if strings.TrimSpace(b.ComputerUseEndpoint) != "" {
			return concurrency.ComputerUseReady
		}
		return concurrency.ComputerUseLoginRequired
	case SandboxStateReady:
		if strings.TrimSpace(b.ComputerUseEndpoint) == "" {
			return concurrency.ComputerUseLoginRequired
		}
		return concurrency.ComputerUseUnavailable
	case SandboxStateProvisioning:
		return concurrency.ComputerUseProvisioning
	case SandboxStateDegraded, SandboxStateResetRequired:
		return concurrency.ComputerUseDegraded
	default:
		return concurrency.ComputerUseUnavailable
	}
}

func (b SandboxBinding) Row() concurrency.SandboxBindingRow {
	return concurrency.SandboxBindingRow{
		SandboxID: b.SandboxID, AgentID: b.AgentID, WorkerID: b.WorkerID,
		Provider: b.Provider, VMName: b.VMName, State: b.State,
		ComputerUseEndpoint: b.ComputerUseEndpoint,
		BootstrapPath:       b.BootstrapPath,
		ConsoleCommand:      b.ConsoleCommand,
		CreatedAt:           b.CreatedAt, UpdatedAt: b.UpdatedAt, LastHealthAt: b.LastHealthAt,
		LastError: b.LastError,
	}
}

type SandboxManager interface {
	EnsureAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	GetAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, bool, error)
	OpenSandboxConsole(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	OpenSandboxBrowser(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	StartSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	SuspendSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	ResetSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	DeleteSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	Health(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	ComputerUseEndpoint(context.Context, SandboxEnsureRequest) (string, error)
}

type LocalSandboxManager struct {
	now func() time.Time
}

func NewLocalSandboxManager(now func() time.Time) *LocalSandboxManager {
	return &LocalSandboxManager{now: now}
}

func (m *LocalSandboxManager) EnsureAgentSandbox(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	if !req.Config.Enabled {
		return SandboxBinding{}, nil
	}
	if err := req.Config.normalize(); err != nil {
		return SandboxBinding{}, err
	}
	req.Config.Provider = strings.TrimSpace(req.Config.Provider)
	if req.Config.Provider == "" {
		req.Config.Provider = SandboxProviderTartMacOSVM
	}
	path, err := sandboxBindingPath(req.HomeDir)
	if err != nil {
		return SandboxBinding{}, err
	}
	b, ok, err := readSandboxBinding(path)
	if err != nil {
		return SandboxBinding{}, err
	}
	now := m.clock().UTC()
	if !ok || b.Provider != req.Config.Provider {
		b = SandboxBinding{
			SandboxID: "sbx-" + shortHash(req.AgentID+"|"+req.Config.Provider),
			AgentID:   req.AgentID, WorkerID: req.WorkerID, Provider: req.Config.Provider,
			VMName:    "ac-agent-" + shortHash(req.AgentID),
			State:     SandboxStateProvisioning,
			CreatedAt: now,
		}
	}
	b.AgentID = req.AgentID
	b.WorkerID = req.WorkerID
	b.Provider = req.Config.Provider
	if b.VMName == "" {
		b.VMName = "ac-agent-" + shortHash(req.AgentID)
	}
	b.ConsoleCommand = "tart run " + b.VMName
	if bp := sandboxBootstrapPath(req.HomeDir); bp != "" {
		b.BootstrapPath = bp
	}
	if err := writeSandboxBootstrap(req.HomeDir, b); err != nil {
		b.State = SandboxStateDegraded
		b.LastError = "write sandbox bootstrap: " + err.Error()
	} else {
		b = m.refreshTartBinding(ctx, b)
	}
	if err := writeSandboxBinding(path, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) GetAgentSandbox(_ context.Context, req SandboxEnsureRequest) (SandboxBinding, bool, error) {
	path, err := sandboxBindingPath(req.HomeDir)
	if err != nil {
		return SandboxBinding{}, false, err
	}
	return readSandboxBinding(path)
}

func (m *LocalSandboxManager) OpenSandboxConsole(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	return m.openSandbox(ctx, req, false)
}

func (m *LocalSandboxManager) OpenSandboxBrowser(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	return m.openSandbox(ctx, req, true)
}

func (m *LocalSandboxManager) openSandbox(ctx context.Context, req SandboxEnsureRequest, browser bool) (SandboxBinding, error) {
	b, err := m.EnsureAgentSandbox(ctx, req)
	if err != nil || b.SandboxID == "" {
		return b, err
	}
	b = m.runTartLifecycleCommand(ctx, b, "open_console")
	if browser && b.State != SandboxStateDegraded {
		b = m.runSandboxBrowserCommand(ctx, b)
	}
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) StartSandbox(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	b, err := m.EnsureAgentSandbox(ctx, req)
	if err != nil || b.SandboxID == "" {
		return b, err
	}
	b = m.runTartLifecycleCommand(ctx, b, "start")
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) SuspendSandbox(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	b, ok, err := m.GetAgentSandbox(ctx, req)
	if err != nil || !ok {
		return b, err
	}
	b = m.runTartLifecycleCommand(ctx, b, "suspend")
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) ResetSandbox(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	b, ok, err := m.GetAgentSandbox(ctx, req)
	if err != nil || !ok {
		return b, err
	}
	b = m.runTartLifecycleCommand(ctx, b, "reset")
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) DeleteSandbox(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	b, ok, err := m.GetAgentSandbox(ctx, req)
	if err != nil || !ok {
		return b, err
	}
	b = m.runTartLifecycleCommand(ctx, b, "delete")
	b.State = SandboxStateDeleted
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) Health(ctx context.Context, req SandboxEnsureRequest) (SandboxBinding, error) {
	b, ok, err := m.GetAgentSandbox(ctx, req)
	if err != nil || !ok {
		return b, err
	}
	b = m.refreshTartBinding(ctx, b)
	if err := m.persistBinding(req.HomeDir, b); err != nil {
		return SandboxBinding{}, err
	}
	return b, nil
}

func (m *LocalSandboxManager) ComputerUseEndpoint(ctx context.Context, req SandboxEnsureRequest) (string, error) {
	b, err := m.Health(ctx, req)
	if err != nil {
		return "", err
	}
	return b.ComputerUseEndpoint, nil
}

func (m *LocalSandboxManager) persistBinding(home string, b SandboxBinding) error {
	path, err := sandboxBindingPath(home)
	if err != nil {
		return err
	}
	return writeSandboxBinding(path, b)
}

func (m *LocalSandboxManager) runTartLifecycleCommand(ctx context.Context, b SandboxBinding, op string) SandboxBinding {
	now := m.clock().UTC()
	b.UpdatedAt = now
	if _, err := exec.LookPath("tart"); err != nil {
		b.State = SandboxStateDegraded
		b.LastError = "tart CLI not found on worker host"
		return b
	}
	var args []string
	switch op {
	case "open_console":
		cmd := exec.CommandContext(ctx, "tart", "run", b.VMName)
		if err := cmd.Start(); err != nil {
			b.State = SandboxStateDegraded
			b.LastError = "tart run console failed: " + err.Error()
			return b
		}
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}
		b.State = SandboxStateRunning
		b.LastError = ""
		return b
	case "start":
		return m.startTartSandbox(ctx, b)
	case "suspend":
		args = []string{"suspend", b.VMName}
	case "reset":
		return m.resetTartSandbox(ctx, b)
	case "delete":
		args = []string{"delete", "--yes", b.VMName}
	default:
		b.State = SandboxStateDegraded
		b.LastError = "unknown sandbox lifecycle op: " + op
		return b
	}
	cmd := exec.CommandContext(ctx, "tart", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if op == "suspend" && tartNotRunningError(string(out)) {
			if state, ok := tartVMState(ctx, b.VMName); ok {
				b.State = state
			} else {
				b.State = SandboxStateSuspended
			}
			b.LastError = ""
			return b
		}
		b.State = SandboxStateDegraded
		b.LastError = strings.TrimSpace(fmt.Sprintf("tart %s failed: %v: %s", op, err, string(out)))
		return b
	}
	switch op {
	case "suspend", "reset":
		b.State = SandboxStateSuspended
	case "delete":
		b.State = SandboxStateDeleted
	}
	b.LastError = ""
	return b
}

func (m *LocalSandboxManager) startTartSandbox(ctx context.Context, b SandboxBinding) SandboxBinding {
	if state, ok := tartVMState(ctx, b.VMName); ok && state == SandboxStateRunning {
		b.State = SandboxStateRunning
		b.LastError = ""
		return b
	}
	started, out := m.runTartUntilRunning(ctx, b.VMName)
	if started.State == SandboxStateRunning {
		b.State = SandboxStateRunning
		b.LastError = ""
		return b
	}
	if tartRestoreFailed(out) {
		_ = exec.CommandContext(ctx, "tart", "stop", b.VMName).Run()
		started, out = m.runTartUntilRunning(ctx, b.VMName)
		if started.State == SandboxStateRunning {
			b.State = SandboxStateRunning
			b.LastError = ""
			return b
		}
	}
	b.State = started.State
	b.LastError = started.LastError
	if b.LastError == "" {
		b.LastError = strings.TrimSpace(out)
	}
	return b
}

func (m *LocalSandboxManager) runTartUntilRunning(ctx context.Context, vmName string) (SandboxBinding, string) {
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, "tart", "run", "--no-graphics", vmName)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return SandboxBinding{State: SandboxStateDegraded, LastError: "tart run failed: " + err.Error()}, output.String()
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	var runningSince time.Time
	for {
		select {
		case err := <-waitCh:
			if state, ok := tartVMState(ctx, vmName); ok && state == SandboxStateRunning {
				return SandboxBinding{State: SandboxStateRunning}, output.String()
			}
			detail := strings.TrimSpace(output.String())
			if err != nil {
				return SandboxBinding{
					State:     SandboxStateDegraded,
					LastError: strings.TrimSpace(fmt.Sprintf("tart run failed: %v: %s", err, detail)),
				}, output.String()
			}
			if detail == "" {
				detail = "tart run exited before VM stayed running"
			}
			return SandboxBinding{State: SandboxStateDegraded, LastError: detail}, output.String()
		case <-ticker.C:
			if state, ok := tartVMState(ctx, vmName); ok && state == SandboxStateRunning {
				if runningSince.IsZero() {
					runningSince = m.clock().UTC()
				}
				if m.clock().UTC().Sub(runningSince) >= 10*time.Second {
					return SandboxBinding{State: SandboxStateRunning}, output.String()
				}
			} else {
				runningSince = time.Time{}
			}
		case <-timer.C:
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			state := SandboxStateDegraded
			if actual, ok := tartVMState(ctx, vmName); ok {
				state = actual
			}
			return SandboxBinding{
				State:     state,
				LastError: "tart run timed out before VM reached running",
			}, output.String()
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return SandboxBinding{State: SandboxStateDegraded, LastError: ctx.Err().Error()}, output.String()
		}
	}
}

func (m *LocalSandboxManager) resetTartSandbox(ctx context.Context, b SandboxBinding) SandboxBinding {
	base := strings.TrimSpace(os.Getenv("AC_TART_BASE_IMAGE"))
	if base == "" {
		b.State = SandboxStateResetRequired
		b.LastError = "AC_TART_BASE_IMAGE is not configured; cannot rebuild sandbox VM"
		return b
	}
	_ = exec.CommandContext(ctx, "tart", "stop", b.VMName).Run()
	_ = exec.CommandContext(ctx, "tart", "delete", "--yes", b.VMName).Run()
	if out, err := exec.CommandContext(ctx, "tart", "clone", base, b.VMName).CombinedOutput(); err != nil {
		b.State = SandboxStateDegraded
		b.LastError = strings.TrimSpace(fmt.Sprintf("tart reset clone failed: %v: %s", err, string(out)))
		return b
	}
	b.ComputerUseEndpoint = ""
	b.ComputerUseEnv = nil
	b.State = SandboxStateReady
	b.LastError = "sandbox VM was rebuilt; browser login and Computer Use setup may need to be repeated"
	return b
}

func (m *LocalSandboxManager) runSandboxBrowserCommand(ctx context.Context, b SandboxBinding) SandboxBinding {
	command := sandboxBrowserCommandEnv(b.AgentID)
	if command == "" {
		b.LastError = "sandbox browser command is not configured; VM desktop was opened"
		return b
	}
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		b.State = SandboxStateDegraded
		b.LastError = strings.TrimSpace(fmt.Sprintf("sandbox browser command failed: %v: %s", err, string(out)))
		return b
	}
	b.State = SandboxStateRunning
	b.LastError = ""
	return b
}

func (m *LocalSandboxManager) refreshTartBinding(ctx context.Context, b SandboxBinding) SandboxBinding {
	now := m.clock().UTC()
	b.UpdatedAt = now
	if _, err := exec.LookPath("tart"); err != nil {
		b.State = SandboxStateDegraded
		b.LastError = "tart CLI not found on worker host"
		return b
	}
	base := strings.TrimSpace(os.Getenv("AC_TART_BASE_IMAGE"))
	if base == "" && b.State == SandboxStateProvisioning {
		b.State = SandboxStateDegraded
		b.LastError = "AC_TART_BASE_IMAGE is not configured"
		return b
	}
	if base != "" && b.State == SandboxStateProvisioning {
		cmd := exec.CommandContext(ctx, "tart", "clone", base, b.VMName)
		if out, err := cmd.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			lower := strings.ToLower(msg)
			if !strings.Contains(lower, "already") && !strings.Contains(lower, "exist") {
				b.State = SandboxStateDegraded
				b.LastError = strings.TrimSpace(fmt.Sprintf("tart clone failed: %v: %s", err, msg))
				return b
			}
		}
	}
	if ep := sandboxEndpointEnv(b.AgentID); ep != "" {
		b.ComputerUseEndpoint = ep
	}
	actualState, hasActualState := tartVMState(ctx, b.VMName)
	if b.ComputerUseEndpoint == "" {
		if hasActualState {
			b.State = actualState
		} else {
			b.State = SandboxStateReady
		}
		b.LastError = "Computer Use endpoint is not configured; open the VM console/browser and complete first-time setup"
	} else {
		if hasActualState {
			b.State = actualState
		} else {
			b.State = SandboxStateRunning
		}
		b.LastError = ""
		b.ComputerUseEnv = map[string]string{
			"NODE_REPL_SANDBOX_ALLOWED_UNIX_SOCKETS": b.ComputerUseEndpoint,
			"SKY_CUA_ENDPOINT":                       b.ComputerUseEndpoint,
			"SKY_CUA_SERVICE_NATIVE_PIPE_PATH":       b.ComputerUseEndpoint,
		}
	}
	b.LastHealthAt = now
	select {
	case <-ctx.Done():
		b.State = SandboxStateDegraded
		b.LastError = ctx.Err().Error()
	default:
	}
	return b
}

func sandboxEndpointEnv(agentID string) string {
	key := "AC_SANDBOX_COMPUTER_USE_ENDPOINT_" + strings.ToUpper(strings.NewReplacer("-", "_", ":", "_").Replace(agentID))
	if ep := strings.TrimSpace(os.Getenv(key)); ep != "" {
		return ep
	}
	return strings.TrimSpace(os.Getenv("AC_SANDBOX_COMPUTER_USE_ENDPOINT"))
}

func sandboxBrowserCommandEnv(agentID string) string {
	key := "AC_SANDBOX_BROWSER_COMMAND_" + strings.ToUpper(strings.NewReplacer("-", "_", ":", "_").Replace(agentID))
	if cmd := strings.TrimSpace(os.Getenv(key)); cmd != "" {
		return cmd
	}
	return strings.TrimSpace(os.Getenv("AC_SANDBOX_BROWSER_COMMAND"))
}

func (m *LocalSandboxManager) clock() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (c SandboxConfig) normalize() error {
	if !c.Enabled {
		return nil
	}
	provider := strings.TrimSpace(c.Provider)
	if provider == "" {
		provider = SandboxProviderTartMacOSVM
	}
	if provider != SandboxProviderTartMacOSVM {
		return ErrUnsupportedSandboxProvider
	}
	return nil
}

func sandboxBindingPath(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("agentruntime: sandbox home required")
	}
	return filepath.Join(home, "sandbox", "binding.json"), nil
}

func sandboxBootstrapPath(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "sandbox", "bootstrap")
}

func writeSandboxBootstrap(home string, b SandboxBinding) error {
	root := sandboxBootstrapPath(home)
	if root == "" {
		return errors.New("agentruntime: sandbox home required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	readme := fmt.Sprintf(`# Agent Center Tart Sandbox Bootstrap

Agent ID: %s
Worker ID: %s
Provider: %s
VM name: %s

This directory is the host-side bootstrap bundle for the agent sandbox. The Tart
base image must not contain LLM auth, browser cookies, Agent Center worker tokens,
or third-party credentials. Copy only the agent-scoped material needed by the VM
guest bootstrap channel.

Console:

    %s

Browser setup is manual in v1: open the VM console, open the browser inside the
guest, and sign in there. Login state must remain inside this agent VM.
`, b.AgentID, b.WorkerID, b.Provider, b.VMName, b.ConsoleCommand)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o600); err != nil {
		return err
	}
	manifest := map[string]any{
		"agent_id":        b.AgentID,
		"worker_id":       b.WorkerID,
		"provider":        b.Provider,
		"vm_name":         b.VMName,
		"console_command": b.ConsoleCommand,
		"created_at":      b.CreatedAt,
		"updated_at":      b.UpdatedAt,
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o600)
}

func readSandboxBinding(path string) (SandboxBinding, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SandboxBinding{}, false, nil
	}
	if err != nil {
		return SandboxBinding{}, false, err
	}
	var out SandboxBinding
	if err := json.Unmarshal(b, &out); err != nil {
		return SandboxBinding{}, false, fmt.Errorf("agentruntime: read sandbox binding: %w", err)
	}
	return out, true, nil
}

func writeSandboxBinding(path string, b SandboxBinding) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
