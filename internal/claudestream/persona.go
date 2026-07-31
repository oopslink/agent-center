package claudestream

import "strings"

// personaDescriptionHeading labels the agent's profile-description persona段 in the
// system prompt. Kept in the same "== … ==" section style as AgentWorkQueueSystemPrompt.
const personaDescriptionHeading = "== About you =="

const centerAccessPolicySection = "== Agent-center access policy ==\n" +
	"Use only the provided agent-center MCP tools for agent-center state reads or writes, including messages, tasks, plans, reminders, files, and agent/runtime status. " +
	"Do not access the agent-center database, SQLite files, admin socket, admin HTTP endpoints, worker tokens, mcp_config.runtime.json, or process arguments as a fallback. " +
	"On each fresh Codex supervisor session, verify the real model tool registry with tool_search for agent-center post_message before doing center-state work. " +
	"If an agent-center MCP tool is missing, unavailable, or fails to load, report that blocker in the current conversation and stop the affected center-state operation."

// PersonaDescriptionSection wraps an agent's profile description as a system-prompt
// persona段 (T728). A blank description yields "" (no section) so an agent without a
// description — or one that opted the injection out (the caller passes "") — adds
// nothing to the prompt.
func PersonaDescriptionSection(description string) string {
	d := strings.TrimSpace(description)
	if d == "" {
		return ""
	}
	return personaDescriptionHeading + "\n" + d
}

// ComposeExtraSystemPrompt joins the optional persona段 (from the agent's profile
// description, ALREADY gated by the per-agent switch upstream — the caller passes ""
// when injection is off) and the memory harness context into the single
// --append-system-prompt extra text carried by BuildStreamingArgv. Either input may
// be empty; present sections are separated by a blank line and the persona段 comes
// FIRST (who-you-are/persona before working memory). Both empty → "".
func ComposeExtraSystemPrompt(promptDescription, memoryContext string) string {
	parts := make([]string, 0, 3)
	parts = append(parts, centerAccessPolicySection)
	if s := PersonaDescriptionSection(promptDescription); s != "" {
		parts = append(parts, s)
	}
	if m := sanitizeMemoryContext(memoryContext); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "\n\n")
}

// sanitizeMemoryContext drops legacy memory lines that taught agents to bypass MCP
// through center internals. It is intentionally conservative: one tainted line is
// removed, not the whole memory document, so ordinary project memory still loads.
func sanitizeMemoryContext(memoryContext string) string {
	m := strings.TrimSpace(memoryContext)
	if m == "" {
		return ""
	}
	lines := strings.Split(m, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if memoryLineBypassesAgentCenterMCP(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func memoryLineBypassesAgentCenterMCP(line string) bool {
	s := strings.ToLower(strings.TrimSpace(line))
	if s == "" {
		return false
	}
	mcpish := strings.Contains(s, "mcp") || strings.Contains(s, "工具")
	bypassish := strings.Contains(s, "fallback") || strings.Contains(s, "兜底") || strings.Contains(s, "bypass") || strings.Contains(s, "绕过")
	centerInternal := strings.Contains(s, "admin-socket") ||
		strings.Contains(s, "admin socket") ||
		strings.Contains(s, "admin.sock") ||
		strings.Contains(s, "/admin/agent-tools") ||
		strings.Contains(s, "agent-center.db") ||
		strings.Contains(s, "sqlite") ||
		strings.Contains(s, "mcp_config.runtime.json") ||
		strings.Contains(s, "ac_mcp_worker_token") ||
		strings.Contains(s, "worker token") ||
		strings.Contains(s, "进程") && strings.Contains(s, "token")
	if centerInternal && (mcpish || bypassish) {
		return true
	}
	if centerInternal && strings.Contains(s, "agent-center") && bypassish {
		return true
	}
	return false
}
