package claudestream

import "strings"

// personaDescriptionHeading labels the agent's profile-description persona段 in the
// system prompt. Kept in the same "== … ==" section style as AgentWorkQueueSystemPrompt.
const personaDescriptionHeading = "== About you =="

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
	parts := make([]string, 0, 2)
	if s := PersonaDescriptionSection(promptDescription); s != "" {
		parts = append(parts, s)
	}
	if m := strings.TrimSpace(memoryContext); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "\n\n")
}
