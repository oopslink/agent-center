package cli

// Help grouping + topic registry (P11 § 3.9 discoverability).
//
// `Group` on Command places top-level commands into one of the buckets
// below; the root `agent-center help` page renders them in `groupOrder()`.
// `helpTopic*` define cross-cutting topic pages users can read via
// `agent-center help <topic>` without remembering a specific subcommand.

// groupOrder is the canonical render order for top-level groups. Names
// not in this list fall under "Other".
func groupOrder() []string {
	return []string{
		"Help & info",
		"Resources",      // channel / agent / secret / input-request / message / conversation / project / issue / task / identity
		"Runtime",        // server / worker / supervisor / bootstrap / dispatch / kill-execution / record-decision / escalate-input-request
		"Observability",  // inspect / query / ps / stats / logs / peek-trace / open-issue
		"Admin",          // admin / migrate
	}
}

// groupTopLevel buckets a slice of top-level commands by .Group.
func groupTopLevel(cmds []*Command) map[string][]*Command {
	out := map[string][]*Command{}
	for _, c := range cmds {
		g := c.Group
		if g == "" {
			g = "Other"
		}
		out[g] = append(out[g], c)
	}
	return out
}

// Topic registry — concise hand-written pages that index sub-areas users
// often ask about by concept rather than command (e.g. "what formats are
// supported" without recalling whether to look on `agent --help` or
// `channel --help`).
var helpTopics = map[string]struct {
	summary string
	body    string
}{
	"format": {
		summary: "Output format conventions (`--format=table|json|text`)",
		body: `format — output format conventions

Every command accepts --format with one of three values:

  table   (default) human-readable; multi-column for list, multi-line
          for show, single success line for action commands.
  json    machine-readable; snake_case keys; stable schema (adding
          fields is non-breaking, removing/renaming requires an ADR).
  text    one canonical id per line — useful for piping into xargs.
          For action commands, behaves the same as table.

` + "`human` is accepted as a backwards-compatible alias of `table`." + `

Examples:
  agent-center channel list --format=json
  agent-center agent list --format=text | xargs -L1 agent-center agent show
  agent-center secret create --name=k --value-file=./key --format=json
`,
	},
	"exit-codes": {
		summary: "Exit code conventions",
		body: `exit-codes — return values

   0  success
   1  business error (see stderr / error.reason)
   2  usage error (invalid flag, missing arg, unknown --format)
  16  optimistic-lock version conflict
  17  not found
  18  invalid state transition
  19  invariant violation
  64  feature not yet implemented
 130  SIGINT (Ctrl-C)
`,
	},
	"identity": {
		summary: "Identity refs and the user/agent/system kinds",
		body: `identity — identity reference syntax

agent-center models actors with the kind:id prefix scheme (ADR-0033):

  user:<id>    e.g. user:hayang  (human operator)
  agent:<id>   e.g. agent:01HXXX (an AgentInstance the system spawned)
  system:<id>  e.g. system:scheduler (internal services)

Wherever you see --created-by, --decided-by, sender-identity, or task
actor fields, use the prefixed form. Bare ids without a kind prefix
are rejected at the boundary.

Examples:
  agent-center channel create --name=alpha
  agent-center input-request respond IR-123 --answer=yes --decided-by=user:hayang
`,
	},
}

// helpTopicOrder returns topics in stable render order.
func helpTopicOrder() []string {
	return []string{"format", "identity", "exit-codes"}
}

// helpTopicSummary returns the one-line description for a topic. Empty
// when the topic isn't registered.
func helpTopicSummary(t string) string {
	if r, ok := helpTopics[t]; ok {
		return r.summary
	}
	return ""
}

// helpTopicBody returns the full body for a topic. ok=false when not
// registered (caller may fall through to command lookup).
func helpTopicBody(t string) (string, bool) {
	if r, ok := helpTopics[t]; ok {
		return r.body, true
	}
	return "", false
}

// topLevelMeta carries the canonical Group + Summary for every top-level
// command. `router.Add(...)` auto-creates intermediate group nodes with
// empty Summary/Group; this map fills them in after registration so the
// root help page renders consistently.
var topLevelMeta = map[string]struct{ Group, Summary string }{
	"help":                   {"Help & info", "Show usage. `help <command>` for a subtree, `help <topic>` for an indexed topic."},
	"version":                {"Help & info", "Show version + build commit."},
	"server":                 {"Runtime", "Run the center daemon (long-lived)."},
	"worker":                 {"Runtime", "Worker daemon commands."},
	"supervisor":             {"Runtime", "Supervisor invocation + retrigger."},
	"bootstrap":              {"Runtime", "Install-time bootstrap checks."},
	"dispatch":               {"Runtime", "Dispatch an envelope onto a worker."},
	"kill-execution":         {"Runtime", "Terminate a running execution."},
	"record-decision":        {"Runtime", "Persist a manual decision record."},
	"escalate-input-request": {"Runtime", "Escalate a pending input request."},
	"open-issue":             {"Runtime", "Open an issue from a message (alias)."},
	"migrate":                {"Admin", "Run pending DB migrations."},
	"admin":                  {"Admin", "Internal / ops commands."},
	"channel":                {"Resources", "Channels (top-level conversation containers)."},
	"agent":                  {"Resources", "Agent instances (create / list / archive)."},
	"secret":                 {"Resources", "User secret CRUD (kind=mcp|cloud_credential|...)."},
	"input-request":          {"Resources", "Pending input requests from executing tasks."},
	"message":                {"Resources", "Message lookups + cross-conv references."},
	"conversation":           {"Resources", "Conversation operations (send / tail / refs)."},
	"task":                   {"Resources", "Task aggregate operations."},
	"issue":                  {"Resources", "Issue aggregate operations."},
	"project":                {"Resources", "Project aggregate operations."},
	"identity":               {"Resources", "Identity registration (user / agent / system kinds)."},
	"inspect":                {"Observability", "Inspect a single entity."},
	"query":                  {"Observability", "Cross-BC query."},
	"ps":                     {"Observability", "List active runtime processes."},
	"stats":                  {"Observability", "Roll-up counters."},
	"logs":                   {"Observability", "Stream / tail logs."},
	"peek-trace":             {"Observability", "Peek a trace by id."},
}

// assignTopLevelMeta fills in Group + Summary on each top-level command
// from topLevelMeta. Skips commands with non-empty fields (preserves any
// explicit overrides set by the registration site).
func assignTopLevelMeta(root *Command) {
	for _, c := range root.Subcommands {
		meta, ok := topLevelMeta[c.Name]
		if !ok {
			continue
		}
		if c.Group == "" {
			c.Group = meta.Group
		}
		if c.Summary == "" {
			c.Summary = meta.Summary
		}
	}
}
