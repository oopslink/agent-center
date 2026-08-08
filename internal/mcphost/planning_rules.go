package mcphost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const (
	planRuleRefreshSemantics  = "plan rules are snapshotted once per MCP planning session; create_plan/edit_plan_topology reuse that frozen snapshot for the session, and a new supervisor generation starts a fresh MCP host process that reloads from the current team repo"
	planRuleSourceMCP         = "mcp_plan_tool"
	planRuleSourceUnavailable = "unavailable"
)

type planningRuleCache struct {
	cfg Config
	mu  sync.Mutex

	loaded bool
	snap   map[string]any
}

func newPlanningRuleCache(cfg Config) *planningRuleCache {
	return &planningRuleCache{cfg: cfg}
}

func (c *planningRuleCache) Snapshot(ctx context.Context) map[string]any {
	if c == nil {
		return unavailablePlanRuleSnapshot(Config{}, "planning rule cache is not wired")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.snap = c.load(ctx)
		c.loaded = true
	}
	return clonePlanRuleMap(c.snap)
}

func (c *planningRuleCache) load(ctx context.Context) map[string]any {
	if c.cfg.Admin == nil {
		return unavailablePlanRuleSnapshot(c.cfg, "admin caller is not wired")
	}
	var raw json.RawMessage
	body := map[string]any{"agent_id": c.cfg.AgentID, "phase": "plan"}
	if err := c.cfg.Admin.CallAgentTool(ctx, "get_team_rules", body, &raw); err != nil {
		return unavailablePlanRuleSnapshot(c.cfg, err.Error())
	}
	if len(raw) == 0 {
		snap := basePlanRuleSnapshot(c.cfg)
		snap["source"] = planRuleSourceMCP
		return snap
	}
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		return unavailablePlanRuleSnapshot(c.cfg, "decode get_team_rules response: "+err.Error())
	}
	if snap == nil {
		snap = map[string]any{}
	}
	annotatePlanRuleSnapshot(c.cfg, snap)
	return snap
}

func annotatePlanRuleSnapshot(cfg Config, snap map[string]any) {
	snap["phase"] = "plan"
	if strings.TrimSpace(stringValue(snap["source"])) == "" {
		snap["source"] = planRuleSourceMCP
	}
	snap["planning_session_id"] = planningSessionID(cfg)
	snap["planning_generation"] = cfg.Generation
	snap["refresh_semantics"] = planRuleRefreshSemantics
	ensurePlanRuleArrays(snap)
}

func unavailablePlanRuleSnapshot(cfg Config, loadErr string) map[string]any {
	snap := basePlanRuleSnapshot(cfg)
	snap["source"] = planRuleSourceUnavailable
	snap["load_error"] = strings.TrimSpace(loadErr)
	return snap
}

func basePlanRuleSnapshot(cfg Config) map[string]any {
	snap := map[string]any{
		"team_id":             "",
		"phase":               "plan",
		"commit":              "",
		"source":              planRuleSourceMCP,
		"planning_session_id": planningSessionID(cfg),
		"planning_generation": cfg.Generation,
		"load_error":          "",
		"rules":               []any{},
		"skipped_nonstandard": []any{},
		"refresh_semantics":   planRuleRefreshSemantics,
	}
	return snap
}

func planningSessionID(cfg Config) string {
	agentID := strings.TrimSpace(cfg.AgentID)
	if agentID == "" {
		agentID = "unknown"
	}
	return fmt.Sprintf("agent:%s/generation:%d", agentID, cfg.Generation)
}

func ensurePlanRuleArrays(snap map[string]any) {
	if _, ok := snap["rules"]; !ok || snap["rules"] == nil {
		snap["rules"] = []any{}
	}
	if _, ok := snap["skipped_nonstandard"]; !ok || snap["skipped_nonstandard"] == nil {
		snap["skipped_nonstandard"] = []any{}
	}
}

func clonePlanRuleMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	return out
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
