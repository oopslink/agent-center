package orchestrator

// issueinline.go — spawn-time inline expansion of issue references (I109 ①).
//
// WHY THIS EXISTS. A forked executor is a FROZEN, ONE-SHOT process: `claude -p`,
// single turn, no MCP, no center credentials (design §4 / F1 isolation, enforced
// by BuildExecutorEnv + the no---mcp-config spawn). It therefore CANNOT read an
// issue. But the orchestration side keeps writing task descriptions that treat it
// like a conversational participant — "见 issue-13e7bfe8 完整证据与根因". To the
// executor that sentence is a DEAD REFERENCE: it names evidence it has no channel
// to fetch. One executor got the root cause right anyway by re-deriving it from
// the prompt body; that was luck, not design.
//
// The pre-existing mitigation was human discipline ("task authors must write
// self-contained briefs"). A convention is not a mechanism — it regresses the
// moment someone else writes the task. So the ORCHESTRATOR (which does hold center
// credentials) resolves the refs and inlines the issue bodies into the prompt
// BEFORE the fork. The executor is untouched: it just receives a prompt that
// happens to already contain what it needs.
//
// THREE RULES, all in service of "never silently degrade":
//  1. BOUNDED — a giant issue must not blow up the prompt (per-issue + total rune
//     caps, and a cap on how many refs are expanded).
//  2. TRUNCATION IS ALWAYS MARKED — both in the prompt (so the READER knows text
//     is missing) and in the log. A silent truncation buys a clean-looking prompt
//     and sells a reader who trusts it; that is the `| head` failure mode.
//  3. FAIL-LOUD — a resolve failure, a missing resolver, a truncation, and a
//     dropped over-cap ref each emit their OWN distinguishable log line, AND leave
//     a visible marker in the prompt. A zero-log skip is the exact bug family this
//     change belongs to (push.go's `c.Git == nil` silent return hid a P0 for a
//     whole cycle four lines under a comment promising "every skip is logged").

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Expansion bounds. Deliberately generous — the point is to fit a real issue body
// into a prompt, not to summarize it — but hard, so one pathological issue cannot
// crowd out the task brief itself.
const (
	// maxInlinedIssueBody bounds ONE issue's description in the prompt.
	maxInlinedIssueBody = 8000
	// maxInlinedTotal bounds the whole inlined section across all refs.
	maxInlinedTotal = 24000
	// maxInlinedIssues bounds how many distinct refs are expanded. Refs beyond it are
	// dropped LOUDLY (logged + marked in the prompt), never quietly.
	maxInlinedIssues = 5
)

// IssueDoc is a resolved issue as the prompt needs it: the ref as written, the
// title, and the body to inline.
type IssueDoc struct {
	Ref   string
	Title string
	Body  string
}

// IssueResolver fetches an issue's text for inline expansion. A PORT: the engine
// lives under the executor package's no-center red line, so the concrete center
// -backed implementation (get_issue / list_issues via the agent-tool transport)
// is injected from internal/agentruntime, which owns the transport.
//
// ref is the reference AS WRITTEN in the task text ("issue-13e7bfe8" or "I109");
// projectID scopes an I<n> org-ref lookup and may be empty (then an I<n> ref is
// unresolvable and must surface as an error, not a silent skip).
type IssueResolver interface {
	ResolveIssue(ctx context.Context, ref, projectID string) (*IssueDoc, error)
}

// issueRefRe matches the two reference forms an author writes: the canonical
// entity id ("issue-<8hex>", idgen NewEntityID shape) and the per-org display ref
// ("I<n>", v2.7.1 #245). \b on both ends keeps "I18n" / "issue-xyz" from matching.
var issueRefRe = regexp.MustCompile(`\bissue-[0-9a-fA-F]{8}\b|\bI[0-9]{1,6}\b`)

// scanIssueRefs returns the distinct issue references in text, in first-appearance
// order (stable prompts across identical work items). Case is preserved as written;
// dedupe is case-insensitive so "Issue-AB12" and "issue-ab12" expand once.
func scanIssueRefs(texts ...string) []string {
	var refs []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, m := range issueRefRe.FindAllString(t, -1) {
			k := strings.ToLower(m)
			if seen[k] {
				continue
			}
			seen[k] = true
			refs = append(refs, m)
		}
	}
	return refs
}

// inlineIssueRefs resolves every issue reference in the work item and renders the
// "## Referenced issues (inlined)" prompt section, or "" when there is nothing to
// expand. It NEVER returns an error: a ref that cannot be expanded degrades to a
// visible UNAVAILABLE marker in the prompt plus a log line, because failing the
// whole fork over an unreadable reference would be worse than shipping the brief
// with an honest gap.
//
// logf is the fail-loud sink (nil → a no-op only for the tests that don't assert
// logs; production always wires one).
func inlineIssueRefs(ctx context.Context, res IssueResolver, item WorkItem, logf func(string, ...any)) string {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	refs := scanIssueRefs(item.Goal.Title, item.Goal.Description, item.Goal.IssueSpec, item.Context)
	if len(refs) == 0 {
		return ""
	}
	// No resolver wired ⇒ the refs stay dead. That is a DEGRADED fork, not a normal
	// one, so it is logged rather than skipped in silence.
	if res == nil {
		logf("orchestrator: issue inline: no resolver wired, %d ref(s) left as dead references (task=%s refs=%v)",
			len(refs), item.TaskRef, refs)
		return ""
	}

	if len(refs) > maxInlinedIssues {
		dropped := refs[maxInlinedIssues:]
		refs = refs[:maxInlinedIssues]
		logf("orchestrator: issue inline: ref cap %d reached, %d ref(s) NOT expanded (task=%s dropped=%v)",
			maxInlinedIssues, len(dropped), item.TaskRef, dropped)
	}

	var b strings.Builder
	b.WriteString("\n\n## Referenced issues (inlined)\n")
	b.WriteString("以下 issue 正文由 orchestrator 在 spawn 前内联展开 —— executor 是一次性进程，无法自行读取 issue。\n")
	budget := maxInlinedTotal
	for _, ref := range refs {
		doc, err := res.ResolveIssue(ctx, ref, item.ProjectID)
		if err != nil {
			// FAIL-LOUD, both channels: the operator gets a log line, and the executor
			// gets an explicit "this reference is unavailable" instead of a sentence
			// pointing at evidence it cannot see.
			logf("orchestrator: issue inline: resolve ref %s FAILED (task=%s): %v", ref, item.TaskRef, err)
			fmt.Fprintf(&b, "\n### %s — UNAVAILABLE\n引用无法展开（%v）。executor 无法自行读取 issue，请只依据本正文中的信息作业。\n", ref, err)
			continue
		}
		if doc == nil {
			logf("orchestrator: issue inline: resolve ref %s returned no document (task=%s)", ref, item.TaskRef)
			fmt.Fprintf(&b, "\n### %s — UNAVAILABLE\n引用无法展开（resolver 未返回正文）。\n", ref)
			continue
		}
		if budget <= 0 {
			logf("orchestrator: issue inline: total budget %d runes exhausted, ref %s NOT expanded (task=%s)",
				maxInlinedTotal, ref, item.TaskRef)
			fmt.Fprintf(&b, "\n### %s — NOT INLINED（总预算 %d 字符已耗尽）\n", ref, maxInlinedTotal)
			continue
		}
		body, cut, orig := clipRunes(doc.Body, minInt(maxInlinedIssueBody, budget))
		budget -= len([]rune(body))
		fmt.Fprintf(&b, "\n### %s — %s\n%s\n", ref, strings.TrimSpace(doc.Title), body)
		if cut {
			// The marker goes in the PROMPT, not just the log: the executor reading this
			// must know the evidence it is looking at is incomplete.
			fmt.Fprintf(&b, "\n**[已截断：仅展开前 %d / %d 字符 —— 正文未完，请勿据此断定 issue 中不存在某内容]**\n",
				len([]rune(body)), orig)
			logf("orchestrator: issue inline: ref %s body TRUNCATED %d→%d runes (task=%s)",
				ref, orig, len([]rune(body)), item.TaskRef)
		}
		logf("orchestrator: issue inline: expanded ref %s (%d runes, truncated=%v, task=%s)",
			ref, len([]rune(body)), cut, item.TaskRef)
	}
	return b.String()
}

// clipRunes trims s to at most n runes, reporting whether it cut and the original
// rune length (so both the prompt marker and the log can state the real size).
func clipRunes(s string, n int) (out string, truncated bool, origLen int) {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r), false, len(r)
	}
	if n < 0 {
		n = 0
	}
	return string(r[:n]), true, len(r)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
