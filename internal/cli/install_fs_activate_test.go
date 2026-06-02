package cli

import (
	"strings"
	"testing"
)

// v2.7 install-upgrade ship-blocker: serviceActivateCmds for launchd must use the
// modern domain-target API (`launchctl bootout` / `bootstrap`), NOT the
// deprecated `launchctl load`/`unload`. On Darwin 25.1.0+ (macOS 26) `launchctl
// load` fails with "Load failed: 5: Input/output error", so the service never
// (re)starts and install + upgrade roll back. The #72 teardown migration already
// moved uninstall to `bootout`; the activate/restart path was missed.
func TestServiceActivateCmds_LaunchdUsesBootstrapNotLoad(t *testing.T) {
	prevDomain := launchdGUIDomain
	defer func() { launchdGUIDomain = prevDomain }()
	launchdGUIDomain = func() string { return "gui/501" }
	sp := servicePaths{
		OS:              "darwin",
		ServiceManager:  "launchd",
		CenterUnitPath:  "/Users/x/Library/LaunchAgents/com.agent-center.center.plist",
		CenterServiceID: "com.agent-center.center",
	}
	got := serviceActivateCmds(sp, sp.CenterServiceID)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2, steps=%v", len(got), got)
	}
	plist := "/Users/x/Library/LaunchAgents/com.agent-center.center.plist"
	wantBootout := "launchctl bootout gui/501 " + plist
	wantBootstrap := "launchctl bootstrap gui/501 " + plist
	if got[0].Cmd != wantBootout {
		t.Fatalf("step0=%q want %q", got[0].Cmd, wantBootout)
	}
	if !got[0].Tolerate {
		t.Fatalf("bootout step must tolerate non-zero exit (service may not be loaded yet on fresh install)")
	}
	if got[1].Cmd != wantBootstrap {
		t.Fatalf("step1=%q want %q", got[1].Cmd, wantBootstrap)
	}
	if got[1].Tolerate {
		t.Fatalf("bootstrap step must NOT tolerate — a failed load should fail the install (so it rolls back instead of silently not starting)")
	}
	for _, s := range got {
		if strings.Contains(s.Cmd, "launchctl load") || strings.Contains(s.Cmd, "launchctl unload") {
			t.Fatalf("deprecated launchctl load/unload present: %q", s.Cmd)
		}
	}
}

func TestServiceActivateCmds_SystemdUnchanged(t *testing.T) {
	sp := servicePaths{
		OS:              "linux",
		ServiceManager:  "systemd",
		UserMode:        true,
		CenterServiceID: "agent-center.service",
	}
	got := serviceActivateCmds(sp, sp.CenterServiceID)
	wantCmds := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable agent-center.service",
		"systemctl --user restart agent-center.service",
	}
	if len(got) != len(wantCmds) {
		t.Fatalf("len(got)=%d want %d, steps=%v", len(got), len(wantCmds), got)
	}
	for i, want := range wantCmds {
		if got[i].Cmd != want {
			t.Errorf("step %d cmd=%q want %q", i, got[i].Cmd, want)
		}
	}
}
