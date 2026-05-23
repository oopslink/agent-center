package workforce

import (
	"testing"
	"time"
)

func freshAIWithConfig(t *testing.T, cli, cfg string) *AgentInstance {
	t.Helper()
	a, err := NewAgentInstance(NewAgentInstanceInput{
		ID:        "01HG",
		Name:      "n",
		AgentCLI:  cli,
		WorkerID:  wid("W-1"),
		Config:    cfg,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckDispatchFeatures_AllSupported(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{"mcp_config":{"servers":{}},"skills":["x"]}`)
	cap := Capability{
		AgentCLI:        "claude-code",
		Detected:        true,
		Enabled:         true,
		SupportsMCP:     true,
		SupportsSkills:  true,
		SupportsSession: true,
	}
	res := CheckDispatchFeatures(a, cap)
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
}

func TestCheckDispatchFeatures_MCPNotSupported(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{"mcp_config":{"servers":{}}}`)
	cap := Capability{AgentCLI: "claude-code", Detected: true, Enabled: true, SupportsMCP: false}
	res := CheckDispatchFeatures(a, cap)
	if res.OK {
		t.Fatal("expected fail")
	}
	if res.Reason != "feature_unsupported" {
		t.Fatalf("reason: %s", res.Reason)
	}
	if res.Message == "" {
		t.Fatal("message required")
	}
}

func TestCheckDispatchFeatures_SkillsNotSupported(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{"skills":["x"]}`)
	cap := Capability{AgentCLI: "claude-code", Detected: true, Enabled: true, SupportsSkills: false}
	res := CheckDispatchFeatures(a, cap)
	if res.OK {
		t.Fatal("expected fail")
	}
	if res.Reason != "feature_unsupported" {
		t.Fatalf("reason: %s", res.Reason)
	}
}

func TestCheckDispatchFeatures_NoMCPAgentNoMCPCap_OK(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{}`)
	cap := Capability{AgentCLI: "claude-code", Detected: true, Enabled: true}
	res := CheckDispatchFeatures(a, cap)
	if !res.OK {
		t.Fatalf("expected OK for empty agent + no features, got %+v", res)
	}
}

func TestCheckDispatchFeatures_AgentCLIMismatch(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{}`)
	cap := Capability{AgentCLI: "codex", Detected: true, Enabled: true}
	res := CheckDispatchFeatures(a, cap)
	if res.OK {
		t.Fatal()
	}
	if res.Reason != "capability_missing" {
		t.Fatalf("reason: %s", res.Reason)
	}
}

func TestCheckDispatchFeatures_NilAgent(t *testing.T) {
	res := CheckDispatchFeatures(nil, Capability{AgentCLI: "x"})
	if res.OK {
		t.Fatal()
	}
}

func TestCheckDispatchFeatures_EmptyCapabilityCLI(t *testing.T) {
	a := freshAIWithConfig(t, "claude-code", `{}`)
	res := CheckDispatchFeatures(a, Capability{})
	if res.OK {
		t.Fatal()
	}
}

func TestAgentInstance_HasMCPConfig_HasSkillsHint(t *testing.T) {
	for _, tc := range []struct {
		cfg     string
		wantMCP bool
		wantSk  bool
	}{
		{`{}`, false, false},
		{`{"mcp_config":{}}`, true, false},
		{`{"skills":[]}`, false, true},
		{`{"mcp_config":{},"skills":["x"]}`, true, true},
		{`{"instructions_ref":"x"}`, false, false},
	} {
		a := freshAIWithConfig(t, "claude-code", tc.cfg)
		if got := a.HasMCPConfig(); got != tc.wantMCP {
			t.Errorf("HasMCPConfig(%q) = %v want %v", tc.cfg, got, tc.wantMCP)
		}
		if got := a.HasSkillsHint(); got != tc.wantSk {
			t.Errorf("HasSkillsHint(%q) = %v want %v", tc.cfg, got, tc.wantSk)
		}
	}
}

func TestAgentInstance_HasMCPConfig_NilSafe(t *testing.T) {
	var a *AgentInstance
	if a.HasMCPConfig() {
		t.Fatal()
	}
	if a.HasSkillsHint() {
		t.Fatal()
	}
}

func TestWorker_CapabilityForCLI(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{
		ID: "W-1", EnrolledAt: time.Now(),
		CapabilityList: []Capability{
			{AgentCLI: "claude-code", Detected: true, Enabled: true, SupportsMCP: true},
			{AgentCLI: "codex", Detected: true, Enabled: false, SupportsMCP: false},
		},
	})
	got, ok := w.CapabilityForCLI("claude-code")
	if !ok || !got.SupportsMCP {
		t.Fatalf("claude-code lookup: %+v ok=%v", got, ok)
	}
	got, ok = w.CapabilityForCLI("codex")
	if !ok || got.Enabled {
		t.Fatalf("codex lookup: %+v ok=%v", got, ok)
	}
	_, ok = w.CapabilityForCLI("gemini")
	if ok {
		t.Fatal("gemini should not be found")
	}
}
