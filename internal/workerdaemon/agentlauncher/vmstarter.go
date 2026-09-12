package agentlauncher

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
)

// TartVMStarter starts the per-agent runtime inside a Tart macOS VM. Host state is
// mounted into the VM with virtio-fs, while the worker still talks to a host-local
// Unix socket that is forwarded to the VM's runtime socket over SSH.
type TartVMStarter struct {
	binaryPath string
	baseArgs   []string
	baseEnv    []string
	homeBase   string
	sockDir    string
	stdout     io.Writer
	stderr     io.Writer
	log        func(format string, args ...any)
}

type TartVMStarterConfig struct {
	BinaryPath string
	BaseArgs   []string
	BaseEnv    []string
	HomeBase   string
	SockDir    string
	Stdout     io.Writer
	Stderr     io.Writer
	Log        func(format string, args ...any)
}

type tartMount struct {
	name string
	host string
}

type tartGuestMountPlan struct {
	mounts           []tartMount
	codexHome        string
	claudeConfigDir  string
	builtinSkillsDir string
}

func NewTartVMStarter(cfg TartVMStarterConfig) (*TartVMStarter, error) {
	bin := strings.TrimSpace(cfg.BinaryPath)
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		bin = self
	}
	if strings.TrimSpace(cfg.HomeBase) == "" {
		return nil, errors.New("agentlauncher: tart vm starter requires home_base")
	}
	if strings.TrimSpace(cfg.SockDir) == "" {
		return nil, errors.New("agentlauncher: tart vm starter requires sock_dir")
	}
	out := cfg.Stdout
	if out == nil {
		out = os.Stdout
	}
	errw := cfg.Stderr
	if errw == nil {
		errw = os.Stderr
	}
	log := cfg.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	return &TartVMStarter{
		binaryPath: bin,
		baseArgs:   append([]string{}, cfg.BaseArgs...),
		baseEnv:    append([]string{}, cfg.BaseEnv...),
		homeBase:   cfg.HomeBase,
		sockDir:    cfg.SockDir,
		stdout:     out,
		stderr:     errw,
		log:        log,
	}, nil
}

var _ ProcessStarter = (*TartVMStarter)(nil)

func (s *TartVMStarter) Start(ctx context.Context, spec AgentSpec) (Process, error) {
	if spec.AgentID == "" {
		return nil, errors.New("agentlauncher: tart vm start requires agent_id")
	}
	if !spec.Sandbox.Enabled || spec.Sandbox.RuntimePlacement != RuntimePlacementVMRuntime {
		return nil, errors.New("agentlauncher: tart vm starter requires vm_runtime placement")
	}
	if strings.TrimSpace(spec.Sandbox.Provider) != "" && spec.Sandbox.Provider != "tart_macos_vm" {
		return nil, fmt.Errorf("agentlauncher: unsupported sandbox provider for vm_runtime: %s", spec.Sandbox.Provider)
	}
	vmName := tartVMName(spec.AgentID)
	agentHome := filepath.Join(s.homeBase, "agents", spec.AgentID)
	if err := os.MkdirAll(agentHome, 0o700); err != nil {
		return nil, fmt.Errorf("agentlauncher: create agent host home: %w", err)
	}
	configPath := tartConfigPath(s.baseArgs)
	mountPlan := s.guestMountPlan(spec.AgentID, agentHome, s.binaryPath, configPath)
	if err := s.ensureVMRunning(ctx, vmName, spec.AgentID, s.binaryPath, configPath, mountPlan); err != nil {
		return nil, err
	}
	ip, err := tartVMIP(ctx, vmName)
	if err != nil {
		return nil, err
	}
	sshIdentity := s.sshIdentityFile(spec.AgentID)
	if err := waitForSSH(ctx, ip, sshIdentity); err != nil {
		return nil, err
	}
	guestSockDir := tartGuestSockDir(spec.AgentID)
	guestSock := filepath.Join(guestSockDir, agentcontrol.SocketName(spec.AgentID))
	hostSock := filepath.Join(s.sockDir, agentcontrol.SocketName(spec.AgentID))
	if err := os.Remove(hostSock); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("agentlauncher: remove stale host socket: %w", err)
	}
	guestBin := tartGuestBinaryPath(s.binaryPath)
	guestArgs := tartGuestRuntimeArgs(s.baseArgs, guestSockDir, configPath, s.homeBase)
	guestArgs = append([]string{"worker", "agent-runtime", "--agent-id", spec.AgentID}, guestArgs...)
	guestArgs = append(guestArgs, spec.Args...)
	guestEnv := tartGuestEnv(s.baseEnv, mountPlan)
	guestEnv = append(guestEnv, spec.Env...)
	if err := s.startGuestRuntime(ctx, ip, sshIdentity, guestBin, guestArgs, guestEnv); err != nil {
		return nil, err
	}
	tunnel, err := s.startControlTunnel(ctx, ip, sshIdentity, hostSock, guestSock)
	if err != nil {
		return nil, err
	}
	if err := waitForControlHealth(ctx, hostSock, spec.AgentID); err != nil {
		s.remoteKill(context.Background(), ip, sshIdentity, spec.AgentID)
		_ = signalProcessGroup(tunnel, syscall.SIGTERM)
		return nil, err
	}
	return &tartVMProcess{
		tunnel:      tunnel,
		vmName:      vmName,
		ip:          ip,
		agentID:     spec.AgentID,
		guestSock:   guestSock,
		hostSock:    hostSock,
		stdout:      s.stdout,
		stderr:      s.stderr,
		guestBin:    guestBin,
		guestArgs:   guestArgs,
		guestEnv:    guestEnv,
		sshIdentity: sshIdentity,
		remoteKill:  s.remoteKill,
	}, nil
}

func (s *TartVMStarter) ensureVMRunning(ctx context.Context, vmName, agentID, binaryPath, configPath string, plan tartGuestMountPlan) error {
	if state, ok := tartGetState(ctx, vmName); ok && state == "running" {
		if s.vmHasRequiredMounts(ctx, vmName, binaryPath, configPath, plan) {
			return nil
		}
		s.log("agentlauncher: tart vm %s is running without required runtime mounts; restarting", vmName)
		if out, err := exec.CommandContext(ctx, "tart", "stop", vmName).CombinedOutput(); err != nil {
			return fmt.Errorf("agentlauncher: stop tart vm %s before remount: %w: %s", vmName, err, strings.TrimSpace(string(out)))
		}
	}
	if _, ok := tartGetState(ctx, vmName); !ok {
		base := strings.TrimSpace(os.Getenv("AC_TART_BASE_IMAGE"))
		if base == "" {
			return fmt.Errorf("agentlauncher: tart vm %s does not exist and AC_TART_BASE_IMAGE is not configured", vmName)
		}
		if out, err := exec.CommandContext(ctx, "tart", "clone", base, vmName).CombinedOutput(); err != nil {
			return fmt.Errorf("agentlauncher: tart clone %s from %s: %w: %s", vmName, base, err, strings.TrimSpace(string(out)))
		}
	}
	args := []string{
		"run", "--no-graphics",
	}
	for _, m := range plan.mounts {
		if strings.TrimSpace(m.name) == "" || strings.TrimSpace(m.host) == "" {
			continue
		}
		args = append(args, "--dir", tartDirShareArg(m.name, m.host))
	}
	args = append(args, vmName)
	cmd := exec.Command("tart", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("agentlauncher: tart run %s: %w", vmName, err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-waitCh:
			if state, ok := tartGetState(ctx, vmName); ok && state == "running" {
				if cmd.Process != nil {
					_ = cmd.Process.Release()
				}
				return nil
			}
			return fmt.Errorf("agentlauncher: tart run exited before %s stayed running: %v: %s", vmName, err, strings.TrimSpace(out.String()))
		case <-tick.C:
			if state, ok := tartGetState(ctx, vmName); ok && state == "running" {
				if cmd.Process != nil {
					_ = cmd.Process.Release()
				}
				return nil
			}
		case <-timer.C:
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return fmt.Errorf("agentlauncher: tart run timed out for %s: %s", vmName, strings.TrimSpace(out.String()))
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return ctx.Err()
		}
	}
}

func (s *TartVMStarter) guestMountPlan(agentID, agentHome, binaryPath, configPath string) tartGuestMountPlan {
	plan := tartGuestMountPlan{mounts: []tartMount{
		{name: "agent-home-" + shortHash(agentID), host: agentHome},
		{name: "agent-center-state", host: s.homeBase},
		{name: "agent-center-bin", host: filepath.Dir(binaryPath)},
	}}
	if strings.TrimSpace(configPath) != "" {
		plan.mounts = append(plan.mounts, tartMount{name: "agent-center-config", host: filepath.Dir(configPath)})
	}
	if h := s.hostCodexHome(); h != "" && dirExists(h) {
		plan.codexHome = tartGuestSharePath("agent-center-codex-source")
		plan.mounts = append(plan.mounts, tartMount{name: "agent-center-codex-source", host: h})
	}
	if h := s.hostClaudeConfigDir(); h != "" && dirExists(h) {
		plan.claudeConfigDir = tartGuestSharePath("agent-center-claude-config")
		plan.mounts = append(plan.mounts, tartMount{name: "agent-center-claude-config", host: h})
	}
	if h := envValue(s.baseEnv, "CLAUDE_BUILTIN_SKILLS_DIR"); h != "" && dirExists(h) {
		plan.builtinSkillsDir = tartGuestSharePath("agent-center-claude-builtin-skills")
		plan.mounts = append(plan.mounts, tartMount{name: "agent-center-claude-builtin-skills", host: h})
	}
	return plan
}

func (s *TartVMStarter) hostCodexHome() string {
	if h := envValue(s.baseEnv, "CODEX_HOME"); h != "" {
		return h
	}
	if hd, err := os.UserHomeDir(); err == nil {
		return filepath.Join(hd, ".codex")
	}
	return ""
}

func (s *TartVMStarter) hostClaudeConfigDir() string {
	if h := envValue(s.baseEnv, "CLAUDE_CONFIG_DIR"); h != "" {
		return h
	}
	if hd, err := os.UserHomeDir(); err == nil {
		return filepath.Join(hd, ".claude")
	}
	return ""
}

func (s *TartVMStarter) vmHasRequiredMounts(ctx context.Context, vmName, binaryPath, configPath string, plan tartGuestMountPlan) bool {
	guestBin := tartGuestBinaryPath(binaryPath)
	checks := []string{"test -x " + shellQuote(guestBin), "test -d " + shellQuote(tartGuestHomeBasePath())}
	if strings.TrimSpace(configPath) != "" {
		checks = append(checks, "test -f "+shellQuote(tartGuestConfigPath(configPath)))
	}
	if plan.codexHome != "" {
		checks = append(checks, "test -d "+shellQuote(plan.codexHome))
	}
	if plan.claudeConfigDir != "" {
		checks = append(checks, "test -d "+shellQuote(plan.claudeConfigDir))
	}
	tests := []string{"sh", "-lc", strings.Join(checks, " && ")}
	out, err := exec.CommandContext(ctx, "tart", append([]string{"exec", vmName}, tests...)...).CombinedOutput()
	if err != nil {
		s.log("agentlauncher: tart vm %s required mount check failed: %v: %s", vmName, err, strings.TrimSpace(string(out)))
		return false
	}
	return true
}

func (s *TartVMStarter) sshIdentityFile(agentID string) string {
	if v := envValue(s.baseEnv, "AC_TART_SSH_IDENTITY_FILE"); v != "" && regularFileExists(v) {
		return v
	}
	key := "AC_SANDBOX_SSH_KEY_FILE_" + strings.ToUpper(strings.NewReplacer("-", "_", ":", "_").Replace(agentID))
	if v := envValue(s.baseEnv, key); v != "" && regularFileExists(v) {
		return v
	}
	runDirKey := filepath.Join(s.homeBase, "agents", agentID, "sandbox", "run", "id_ed25519")
	if regularFileExists(runDirKey) {
		return runDirKey
	}
	return ""
}

func (s *TartVMStarter) startGuestRuntime(ctx context.Context, ip, identityFile, guestBin string, args, env []string) error {
	remote := "mkdir -p " + shellQuote(tartGuestSockDirFromArgs(args)) + " && nohup " + shellJoin(append([]string{guestBin}, args...), env) + " >/tmp/agent-center-runtime.log 2>&1 &"
	cmd := exec.CommandContext(ctx, "ssh", sshBaseArgs(ip, identityFile, "sh", "-lc", remote)...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("agentlauncher: start guest agent-runtime: %w", err)
	}
	return nil
}

func (s *TartVMStarter) startControlTunnel(ctx context.Context, ip, identityFile, hostSock, guestSock string) (*exec.Cmd, error) {
	args := []string{
		"-N",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-L", hostSock + ":" + guestSock,
	}
	if strings.TrimSpace(identityFile) != "" {
		args = append(args, "-i", identityFile)
	}
	args = append(args, "admin@"+ip)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("agentlauncher: start control tunnel: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil, errors.New("agentlauncher: control tunnel exited during startup")
	}
	return cmd, nil
}

func (s *TartVMStarter) remoteKill(ctx context.Context, ip, identityFile, agentID string) {
	pattern := "worker agent-runtime --agent-id " + agentID
	cmd := exec.CommandContext(ctx, "ssh", sshBaseArgs(ip, identityFile, "pkill", "-TERM", "-f", pattern)...)
	_ = cmd.Run()
}

type tartVMProcess struct {
	tunnel      *exec.Cmd
	vmName      string
	ip          string
	agentID     string
	guestSock   string
	hostSock    string
	stdout      io.Writer
	stderr      io.Writer
	guestBin    string
	guestArgs   []string
	guestEnv    []string
	sshIdentity string
	remoteKill  func(context.Context, string, string, string)
}

func (p *tartVMProcess) Wait() error {
	tunnelDone := make(chan error, 1)
	go func() { tunnelDone <- p.tunnel.Wait() }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	client := agentcontrol.NewClient(p.hostSock, 2*time.Second)
	misses := 0
	for {
		select {
		case err := <-tunnelDone:
			return err
		case <-ticker.C:
			got, err := client.Probe(context.Background())
			if err == nil && got == p.agentID {
				misses = 0
				continue
			}
			misses++
			if misses >= 3 {
				_ = p.signalTunnel(syscall.SIGTERM)
				if err != nil {
					return fmt.Errorf("agentlauncher: vm_runtime health failed: %w", err)
				}
				return fmt.Errorf("agentlauncher: vm_runtime health served %q, want %q", got, p.agentID)
			}
		}
	}
}
func (p *tartVMProcess) PID() int {
	if p.tunnel == nil || p.tunnel.Process == nil {
		return 0
	}
	return p.tunnel.Process.Pid
}
func (p *tartVMProcess) Signal() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.remoteKill != nil {
		p.remoteKill(ctx, p.ip, p.sshIdentity, p.agentID)
	}
	return p.signalTunnel(syscall.SIGTERM)
}
func (p *tartVMProcess) Kill() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.remoteKill != nil {
		p.remoteKill(ctx, p.ip, p.sshIdentity, p.agentID)
	}
	return p.signalTunnel(syscall.SIGKILL)
}
func (p *tartVMProcess) signalTunnel(sig syscall.Signal) error {
	if p.tunnel == nil || p.tunnel.Process == nil {
		return nil
	}
	return signalProcessGroup(p.tunnel, sig)
}

func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, sig); err != nil {
		return cmd.Process.Signal(sig)
	}
	return nil
}

func waitForControlHealth(ctx context.Context, hostSock, agentID string) error {
	deadline := time.Now().Add(20 * time.Second)
	client := agentcontrol.NewClient(hostSock, time.Second)
	var last error
	for time.Now().Before(deadline) {
		got, err := client.Probe(ctx)
		if err == nil && got == agentID {
			return nil
		}
		if err == nil {
			last = fmt.Errorf("agentlauncher: control health served %q, want %q", got, agentID)
		} else {
			last = err
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if last != nil {
		return fmt.Errorf("agentlauncher: vm_runtime control health timeout: %w", last)
	}
	return errors.New("agentlauncher: vm_runtime control health timeout")
}

func waitForSSH(ctx context.Context, ip, identityFile string) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "ssh", sshBaseArgs(ip, identityFile, "true")...)
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if last != nil {
		return fmt.Errorf("agentlauncher: tart vm ssh not ready at %s: %w", ip, last)
	}
	return fmt.Errorf("agentlauncher: tart vm ssh not ready at %s", ip)
}

func tartGuestRuntimeArgs(base []string, guestSockDir, configPath, homeBase string) []string {
	out := append([]string{}, base...)
	for i := 0; i < len(out); i++ {
		if out[i] == "--sock-dir" && i+1 < len(out) {
			out[i+1] = guestSockDir
			i++
			continue
		}
		if out[i] == "--config" && i+1 < len(out) && strings.TrimSpace(configPath) != "" {
			out[i+1] = tartGuestConfigPath(configPath)
			i++
			continue
		}
		if out[i] == "--admin-target" && i+1 < len(out) {
			out[i+1] = tartGuestAdminTarget(out[i+1])
			i++
		}
	}
	if strings.TrimSpace(homeBase) != "" {
		out = append(out, "--agent-home-base", tartGuestHomeBasePath())
	}
	return out
}

func tartGuestAdminTarget(target string) string {
	override := strings.TrimSpace(os.Getenv("AC_SANDBOX_HOST_ADMIN_TARGET"))
	if override != "" {
		return override
	}
	t := strings.TrimSpace(target)
	for _, prefix := range []string{"http://127.0.0.1:", "https://127.0.0.1:", "http://localhost:", "https://localhost:"} {
		if strings.HasPrefix(t, prefix) {
			scheme := strings.SplitN(prefix, "://", 2)[0]
			return scheme + "://192.168.64.1:" + strings.TrimPrefix(t, prefix)
		}
	}
	return t
}

func tartConfigPath(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--config" {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}

func tartGuestSockDir(agentID string) string {
	return filepath.Join("/tmp", "agent-center", "agent-runtime-"+shortHash(agentID))
}

func tartGuestSockDirFromArgs(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--sock-dir" {
			return args[i+1]
		}
	}
	return "/tmp/agent-center"
}

func tartGuestBinaryPath(hostBinary string) string {
	return filepath.Join("/Volumes/My Shared Files", "agent-center-bin", filepath.Base(hostBinary))
}

func tartGuestConfigPath(hostConfig string) string {
	if strings.TrimSpace(hostConfig) == "" {
		return ""
	}
	return filepath.Join("/Volumes/My Shared Files", "agent-center-config", filepath.Base(hostConfig))
}

func tartGuestHomeBasePath() string {
	return tartGuestSharePath("agent-center-state")
}

func tartGuestSharePath(name string) string {
	return filepath.Join("/Volumes/My Shared Files", name)
}

func tartDirShareArg(name, path string) string {
	return name + ":" + path
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func regularFileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func tartVMName(agentID string) string {
	return "ac-agent-" + shortHash(agentID)
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func tartGetState(ctx context.Context, vmName string) (string, bool) {
	out, err := exec.CommandContext(ctx, "tart", "get", vmName, "--format", "json").CombinedOutput()
	if err != nil {
		return "", false
	}
	var info struct {
		Running bool   `json:"Running"`
		State   string `json:"State"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return "", false
	}
	if info.Running {
		return "running", true
	}
	switch strings.ToLower(strings.TrimSpace(info.State)) {
	case "running":
		return "running", true
	case "suspended":
		return "suspended", true
	case "stopped":
		return "ready", true
	default:
		return "", false
	}
}

func tartVMIP(ctx context.Context, vmName string) (string, error) {
	out, err := exec.CommandContext(ctx, "tart", "ip", vmName).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("agentlauncher: tart ip %s: %w: %s", vmName, err, strings.TrimSpace(string(out)))
	}
	ip := strings.TrimSpace(string(out))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("agentlauncher: tart ip %s returned invalid ip %q", vmName, ip)
	}
	return ip, nil
}

func sshBaseArgs(ip, identityFile string, remote ...string) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}
	if strings.TrimSpace(identityFile) != "" {
		args = append(args, "-i", identityFile)
	}
	args = append(args, "admin@"+ip)
	return append(args, remote...)
}

func defaultGuestEnv() []string {
	return []string{"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"}
}

func tartGuestEnv(base []string, plan tartGuestMountPlan) []string {
	env := withoutEnvKeys(base, "PATH", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "CLAUDE_BUILTIN_SKILLS_DIR")
	env = append(defaultGuestEnv(), env...)
	if plan.codexHome != "" {
		env = append(env, "CODEX_HOME="+plan.codexHome)
	}
	if plan.claudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+plan.claudeConfigDir)
	}
	if plan.builtinSkillsDir != "" {
		env = append(env, "CLAUDE_BUILTIN_SKILLS_DIR="+plan.builtinSkillsDir)
	}
	return env
}

func withoutEnvKeys(env []string, keys ...string) []string {
	block := map[string]struct{}{}
	for _, key := range keys {
		block[key] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, banned := block[name]; banned {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(entry, prefix))
		}
	}
	return ""
}

func shellJoin(argv, env []string) string {
	parts := []string{"env"}
	for _, entry := range env {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		parts = append(parts, shellQuote(entry))
	}
	for _, arg := range argv {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
