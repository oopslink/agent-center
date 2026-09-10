package agentruntime

import (
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
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	LastHealthAt        time.Time         `json:"last_health_at,omitempty"`
	LastError           string            `json:"last_error,omitempty"`
}

func (b SandboxBinding) ComputerUseStatus() string {
	switch b.State {
	case SandboxStateReady, SandboxStateRunning:
		if strings.TrimSpace(b.ComputerUseEndpoint) != "" {
			return concurrency.ComputerUseReady
		}
		return concurrency.ComputerUseLoginRequired
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
		CreatedAt:           b.CreatedAt, UpdatedAt: b.UpdatedAt, LastHealthAt: b.LastHealthAt,
		LastError: b.LastError,
	}
}

type SandboxManager interface {
	EnsureAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error)
	GetAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, bool, error)
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
	b = m.refreshTartBinding(ctx, b)
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
	case "start":
		cmd := exec.CommandContext(ctx, "tart", "run", "--no-graphics", b.VMName)
		if err := cmd.Start(); err != nil {
			b.State = SandboxStateDegraded
			b.LastError = "tart run failed: " + err.Error()
			return b
		}
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}
		b.State = SandboxStateRunning
		b.LastError = ""
		return b
	case "suspend":
		args = []string{"suspend", b.VMName}
	case "reset":
		args = []string{"stop", b.VMName}
	case "delete":
		args = []string{"delete", "--yes", b.VMName}
	default:
		b.State = SandboxStateDegraded
		b.LastError = "unknown sandbox lifecycle op: " + op
		return b
	}
	cmd := exec.CommandContext(ctx, "tart", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
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
	if b.ComputerUseEndpoint == "" {
		b.State = SandboxStateReady
		b.LastError = "Computer Use endpoint is not configured; open the VM console/browser and complete first-time setup"
	} else {
		b.State = SandboxStateRunning
		b.LastError = ""
		b.ComputerUseEnv = map[string]string{"SKY_CUA_ENDPOINT": b.ComputerUseEndpoint}
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
