package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v2.7.1 #211: instance-name validation (kebab-case, 1-32, single interior dashes).
func TestValidInstanceName(t *testing.T) {
	ok := []string{"default", "t1", "test-1", "a", strings.Repeat("a", 32), "my-long-instance-name"}
	for _, n := range ok {
		if !validInstanceName(n) {
			t.Errorf("want valid: %q", n)
		}
	}
	bad := []string{"", strings.Repeat("a", 33), "T1", "-x", "x-", "a_b", "a.b", "a b", "café"}
	for _, n := range bad {
		if validInstanceName(n) {
			t.Errorf("want invalid: %q", n)
		}
	}
}

// v2.7.1 #211: the default instance keeps the legacy prefix; a named instance
// appends ".<name>".
func TestDefaultCenterInstallPrefix(t *testing.T) {
	base := defaultInstallPrefix(true)
	if got := defaultCenterInstallPrefix(true, "default"); got != base {
		t.Errorf("default instance prefix = %q, want legacy %q", got, base)
	}
	if got := defaultCenterInstallPrefix(true, ""); got != base {
		t.Errorf("empty instance prefix = %q, want legacy %q", got, base)
	}
	if got, want := defaultCenterInstallPrefix(true, "t1"), base+".t1"; got != want {
		t.Errorf("named instance prefix = %q, want %q", got, want)
	}
}

// v2.7.1 #211: the center launchd label / unit path is namespaced per instance;
// default keeps the legacy label (back-compat).
func TestApplyInstanceToServicePaths(t *testing.T) {
	launchd := servicePaths{
		ServiceManager:  "launchd",
		CenterServiceID: "com.agent-center.center",
		CenterUnitPath:  "/Users/x/Library/LaunchAgents/com.agent-center.center.plist",
	}
	// default → unchanged.
	if got := applyInstanceToServicePaths(launchd, "default"); got.CenterServiceID != "com.agent-center.center" {
		t.Errorf("default label changed: %q", got.CenterServiceID)
	}
	if got := applyInstanceToServicePaths(launchd, ""); got.CenterServiceID != "com.agent-center.center" {
		t.Errorf("empty label changed: %q", got.CenterServiceID)
	}
	// named → namespaced label + unit path.
	got := applyInstanceToServicePaths(launchd, "t1")
	if got.CenterServiceID != "com.agent-center.center.t1" {
		t.Errorf("label = %q, want com.agent-center.center.t1", got.CenterServiceID)
	}
	if !strings.HasSuffix(got.CenterUnitPath, "/com.agent-center.center.t1.plist") {
		t.Errorf("unit path = %q", got.CenterUnitPath)
	}

	systemd := servicePaths{
		ServiceManager:  "systemd",
		CenterServiceID: "agent-center.service",
		CenterUnitPath:  "/home/x/.config/systemd/user/agent-center.service",
	}
	g2 := applyInstanceToServicePaths(systemd, "t2")
	if g2.CenterServiceID != "agent-center-t2.service" {
		t.Errorf("systemd label = %q, want agent-center-t2.service", g2.CenterServiceID)
	}
	if !strings.HasSuffix(g2.CenterUnitPath, "/agent-center-t2.service") {
		t.Errorf("systemd unit path = %q", g2.CenterUnitPath)
	}
}

// v2.7.1 #211: the generated config carries the instance + the configurable
// server port.
func TestCenterConfigYAML_InstanceAndServerPort(t *testing.T) {
	yaml := centerConfigYAML("/var/data", 7105, 7055, "0.0.0.0:7305", "", "/var/data/master.key", "t1")
	for _, want := range []string{
		`instance: "t1"`,
		`listen_addr: ":7055"`, // server port
		`127.0.0.1:7105`,       // web port
		`admin_tcp_listen: "0.0.0.0:7305"`,
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("missing %q in:\n%s", want, yaml)
		}
	}
}

func TestPortOf(t *testing.T) {
	cases := map[string]string{
		":7050":          "7050",
		"0.0.0.0:7300":   "7300",
		"127.0.0.1:7100": "7100",
		"":               "-",
	}
	for in, want := range cases {
		if got := portOf(in); got != want {
			t.Errorf("portOf(%q)=%q want %q", in, got, want)
		}
	}
}

// v2.7.1 #211: discoverLocalCenters finds every <base>[.<instance>] dir that has
// an etc/config.yaml, reading the instance + ports from each config.
func TestDiscoverLocalCenters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeCfg := func(prefix, instance, serverPort, webPort string) {
		dir := filepath.Join(prefix, "etc")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "server:\n" +
			"  instance: \"" + instance + "\"\n" +
			"  listen_addr: \":" + serverPort + "\"\n" +
			"  sqlite_path: \"" + filepath.Join(prefix, "var", "agent-center.db") + "\"\n" +
			"  admin_tcp_listen: \"0.0.0.0:7300\"\n" +
			"web_console:\n" +
			"  listen_addr: \"127.0.0.1:" + webPort + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := defaultInstallPrefix(true) // ~/.agent-center under temp HOME
	writeCfg(base, "default", "7050", "7100")
	writeCfg(base+".t1", "t1", "7055", "7105")
	// A sibling dir WITHOUT a config must be ignored.
	if err := os.MkdirAll(base+".bogus", 0o755); err != nil {
		t.Fatal(err)
	}

	centers, err := discoverLocalCenters(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(centers) != 2 {
		t.Fatalf("found %d centers, want 2: %+v", len(centers), centers)
	}
	byInst := map[string]localCenter{}
	for _, c := range centers {
		byInst[c.Instance] = c
	}
	if d, ok := byInst["default"]; !ok || d.Prefix != base || d.ServerPort != "7050" || d.WebPort != "7100" {
		t.Errorf("default center wrong: %+v", d)
	}
	if t1, ok := byInst["t1"]; !ok || t1.Prefix != base+".t1" || t1.ServerPort != "7055" || t1.WebPort != "7105" {
		t.Errorf("t1 center wrong: %+v", t1)
	}
}
