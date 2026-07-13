package mcphost

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// team_tools.go registers the Team BC agent tools on the per-agent MCP catalog
// (Team Phase-1 wiring, design §4/§6/§7/§9). Each handler injects the process-
// fixed agent_id and proxies to /admin/agent-tools/<name> via callAdmin — the
// same seam every other agent tool rides. The owning org is resolved center-side
// from the agent, never supplied here.

// registerTeamTools adds the whole Team tool family to srv.
func registerTeamTools(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_team",
		Description: "Create a team in your organization with its template-declared roles. Roles are NOT hardcoded — each role carries its own cli / model / capability_tags / max_concurrency. The team name must be unique within your org.",
	}, makeCreateTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_team",
		Description: "Update a team's name and/or description by team_id. Omit a field to leave it unchanged.",
	}, makeUpdateTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_team",
		Description: "Delete a team by team_id (cascades its roles, members and project associations).",
	}, makeDeleteTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_team",
		Description: "Read a team by team_id, including its declared roles and per-role config.",
	}, makeGetTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_teams",
		Description: "List the teams in your organization.",
	}, makeListTeams(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_member",
		Description: "Add a member to a team under a role the team declared. member_ref is an identity ref: agent:<id> or user:<id>. An agent belongs to at most one team (exclusivity); a human may join many.",
	}, makeAddMember(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "remove_member",
		Description: "Remove a member (member_ref) from a team.",
	}, makeRemoveMember(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "associate_project",
		Description: "Associate a project with a team (many-to-many).",
	}, makeAssociateProject(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_team_template",
		Description: "Author + validate a team template: role composition (each role with a count/配比 and its cli/model/capability_tags), an optional referenced workflow template, and portable (team/global-scope) experiences. Returns the normalized template. Templates are org-level snapshots you then instantiate onto a project.",
	}, makeCreateTeamTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "curate_team_template",
		Description: "Mark a team template as CURATED after you have manually reviewed it (design §9: curation is load-bearing). Export refuses an un-curated template, so run this on an extracted/authored draft before export_team_template. Returns the template with curated=true.",
	}, makeCurateTeamTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "export_team_template",
		Description: "Export a team template to a shareable JSON document (design §6 import/export path). ENFORCES the curation gate: an un-curated template is refused (curate it first). The document can be imported into another org — the cross-org sharing mechanism. Set curated=true once the template has passed manual review.",
	}, makeExportTeamTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "import_team_template",
		Description: "Import a team template from an exported JSON document (design §6): re-homes it into YOUR organization as a fresh, un-curated template. This is how a template shared from another org lands here. Re-review + curate before you re-export it.",
	}, makeImportTeamTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "instantiate_team",
		Description: "Instantiate a team template into your organization: creates the team + its role composition, builds one REAL agent identity per role*count and binds them as members, provisions the team's center-hosted memory repo and seeds it with the template's portable experiences. The team is PROJECT-INDEPENDENT — to bind it to one or more projects, call associate_project separately. Returns the team, the new agent identities, and a SEPARATE runtime-provisioning plan (enroll each agent) — the template carries no runtime/auth.",
	}, makeInstantiateTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "extract_from_team",
		Description: "Snapshot a LIVE team into a DRAFT team template (design §6): copies its role composition and keeps only the portable (team/global-scope) experiences from the team's center-hosted memory, dropping project-scoped facts. Runs a scrub pass that HIGHLIGHTS suspected proprietary tokens (repo/code names, paths, URLs) for you to review — it does NOT auto-remove them. The draft is NOT export-ready: manual curation is still required before you author it as a template.",
	}, makeExtractFromTeam(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "assign_roles",
		Description: "Resolve plan-node roles to concrete agents off a team's current roster (author-time role→agent binding, design §7). Each request names a node_key + role (+ optional avoid_nodes for a Review≠Dev cross-review constraint); strategy is least_busy (default) or round_robin. Returns each node's resolved agent.",
	}, makeAssignRoles(cfg))
}

// ---- create_team ------------------------------------------------------------

type teamRoleArg struct {
	Role           string   `json:"role" jsonschema:"role name (template-defined, not hardcoded)"`
	CLI            string   `json:"cli,omitempty" jsonschema:"agent CLI the role runs on (e.g. claude-code)"`
	Model          string   `json:"model,omitempty" jsonschema:"model id the role uses"`
	CapabilityTags []string `json:"capability_tags,omitempty" jsonschema:"capability requirements for the role"`
	MaxConcurrency int      `json:"max_concurrency,omitempty" jsonschema:"max concurrent members of this role (default 1)"`
}

type createTeamArgs struct {
	Name        string        `json:"name" jsonschema:"team name (unique within your org)"`
	Description string        `json:"description,omitempty" jsonschema:"optional team description"`
	Roles       []teamRoleArg `json:"roles,omitempty" jsonschema:"the team's declared roles"`
}

func makeCreateTeam(cfg Config) mcp.ToolHandlerFor[createTeamArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createTeamArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "create_team", map[string]any{
			"agent_id": cfg.AgentID, "name": args.Name, "description": args.Description, "roles": args.Roles,
		})
	}
}

// ---- update_team ------------------------------------------------------------

type updateTeamArgs struct {
	TeamID      string  `json:"team_id" jsonschema:"the team to update"`
	Name        *string `json:"name,omitempty" jsonschema:"new team name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
}

func makeUpdateTeam(cfg Config) mcp.ToolHandlerFor[updateTeamArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateTeamArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID, "team_id": args.TeamID}
		if args.Name != nil {
			body["name"] = *args.Name
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}
		return callAdmin(ctx, cfg, "update_team", body)
	}
}

// ---- delete_team / get_team -------------------------------------------------

type teamIDArgs struct {
	TeamID string `json:"team_id" jsonschema:"the team id"`
}

func makeDeleteTeam(cfg Config) mcp.ToolHandlerFor[teamIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args teamIDArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "delete_team", map[string]any{"agent_id": cfg.AgentID, "team_id": args.TeamID})
	}
}

func makeGetTeam(cfg Config) mcp.ToolHandlerFor[teamIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args teamIDArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "get_team", map[string]any{"agent_id": cfg.AgentID, "team_id": args.TeamID})
	}
}

// ---- list_teams -------------------------------------------------------------

type listTeamsArgs struct{}

func makeListTeams(cfg Config) mcp.ToolHandlerFor[listTeamsArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listTeamsArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "list_teams", map[string]any{"agent_id": cfg.AgentID})
	}
}

// ---- add_member / remove_member ---------------------------------------------

type addMemberArgs struct {
	TeamID    string `json:"team_id" jsonschema:"the team to add to"`
	MemberRef string `json:"member_ref" jsonschema:"identity ref: agent:<id> or user:<id>"`
	Role      string `json:"role" jsonschema:"a role the team declared"`
}

func makeAddMember(cfg Config) mcp.ToolHandlerFor[addMemberArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args addMemberArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "add_member", map[string]any{
			"agent_id": cfg.AgentID, "team_id": args.TeamID, "member_ref": args.MemberRef, "role": args.Role,
		})
	}
}

type removeMemberArgs struct {
	TeamID    string `json:"team_id" jsonschema:"the team to remove from"`
	MemberRef string `json:"member_ref" jsonschema:"identity ref: agent:<id> or user:<id>"`
}

func makeRemoveMember(cfg Config) mcp.ToolHandlerFor[removeMemberArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args removeMemberArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "remove_member", map[string]any{
			"agent_id": cfg.AgentID, "team_id": args.TeamID, "member_ref": args.MemberRef,
		})
	}
}

// ---- associate_project ------------------------------------------------------

type associateProjectArgs struct {
	TeamID    string `json:"team_id" jsonschema:"the team"`
	ProjectID string `json:"project_id" jsonschema:"the project to associate"`
}

func makeAssociateProject(cfg Config) mcp.ToolHandlerFor[associateProjectArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args associateProjectArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "associate_project", map[string]any{
			"agent_id": cfg.AgentID, "team_id": args.TeamID, "project_id": args.ProjectID,
		})
	}
}

// ---- template authoring / instantiation -------------------------------------

type teamRoleSlotArg struct {
	Role           string   `json:"role" jsonschema:"role name"`
	CLI            string   `json:"cli,omitempty" jsonschema:"agent CLI the role runs on"`
	Model          string   `json:"model,omitempty" jsonschema:"model id the role uses"`
	CapabilityTags []string `json:"capability_tags,omitempty" jsonschema:"capability requirements"`
	MaxConcurrency int      `json:"max_concurrency,omitempty" jsonschema:"max concurrent members (default 1)"`
	Count          int      `json:"count,omitempty" jsonschema:"how many agents to instantiate for this role (配比, default 1)"`
}

type teamExperienceArg struct {
	Slug        string   `json:"slug" jsonschema:"path-safe stem for the experience file"`
	Title       string   `json:"title,omitempty" jsonschema:"optional heading"`
	Description string   `json:"description" jsonschema:"one-line hook (seeds the memory index)"`
	Body        string   `json:"body,omitempty" jsonschema:"markdown content"`
	Scope       string   `json:"scope" jsonschema:"team | global (project scope is not portable)"`
	Tags        []string `json:"tags,omitempty" jsonschema:"optional tags"`
}

type createTeamTemplateArgs struct {
	Name                string              `json:"name" jsonschema:"template name"`
	Description         string              `json:"description,omitempty" jsonschema:"optional description"`
	Roles               []teamRoleSlotArg   `json:"roles" jsonschema:"role composition + per-role config"`
	WorkflowTemplateRef string              `json:"workflow_template_ref,omitempty" jsonschema:"referenced workflow template"`
	Experiences         []teamExperienceArg `json:"experiences,omitempty" jsonschema:"portable (team/global) experiences to carry"`
}

func (a createTeamTemplateArgs) body(cfg Config) map[string]any {
	return map[string]any{
		"agent_id": cfg.AgentID, "name": a.Name, "description": a.Description,
		"roles": a.Roles, "workflow_template_ref": a.WorkflowTemplateRef, "experiences": a.Experiences,
	}
}

func makeCreateTeamTemplate(cfg Config) mcp.ToolHandlerFor[createTeamTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createTeamTemplateArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "create_team_template", args.body(cfg))
	}
}

// ---- curate / export / import (design §6 import/export path + §9 curation) ----

type curateTeamTemplateArgs struct {
	Template createTeamTemplateArgs `json:"template" jsonschema:"the team template to mark curated (after human review)"`
}

func makeCurateTeamTemplate(cfg Config) mcp.ToolHandlerFor[curateTeamTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args curateTeamTemplateArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "curate_team_template", map[string]any{
			"agent_id": cfg.AgentID, "template": args.Template.body(cfg),
		})
	}
}

type exportTeamTemplateArgs struct {
	Template createTeamTemplateArgs `json:"template" jsonschema:"the team template to export"`
	Curated  bool                   `json:"curated,omitempty" jsonschema:"must be true — export refuses an un-curated template (curate it first)"`
}

func makeExportTeamTemplate(cfg Config) mcp.ToolHandlerFor[exportTeamTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args exportTeamTemplateArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "export_team_template", map[string]any{
			"agent_id": cfg.AgentID, "template": args.Template.body(cfg), "curated": args.Curated,
		})
	}
}

type importTeamTemplateArgs struct {
	Document any `json:"document" jsonschema:"the exported team-template JSON document (from export_team_template)"`
}

func makeImportTeamTemplate(cfg Config) mcp.ToolHandlerFor[importTeamTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args importTeamTemplateArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "import_team_template", map[string]any{
			"agent_id": cfg.AgentID, "document": args.Document,
		})
	}
}

type instantiateTeamArgs struct {
	TeamName string                 `json:"team_name,omitempty" jsonschema:"name for the instantiated team (defaults to the template name)"`
	Template createTeamTemplateArgs `json:"template" jsonschema:"the team template to instantiate"`
}

func makeInstantiateTeam(cfg Config) mcp.ToolHandlerFor[instantiateTeamArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args instantiateTeamArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "instantiate_team", map[string]any{
			"agent_id": cfg.AgentID, "team_name": args.TeamName,
			"template": args.Template.body(cfg),
		})
	}
}

// ---- extract_from_team ------------------------------------------------------

type extractFromTeamArgs struct {
	TeamID string         `json:"team_id" jsonschema:"the live team to extract a template draft from"`
	Counts map[string]int `json:"counts,omitempty" jsonschema:"optional per-role instance counts (role → count) for the draft"`
}

func makeExtractFromTeam(cfg Config) mcp.ToolHandlerFor[extractFromTeamArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args extractFromTeamArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "extract_from_team", map[string]any{
			"agent_id": cfg.AgentID, "team_id": args.TeamID, "counts": args.Counts,
		})
	}
}

// ---- assign_roles -----------------------------------------------------------

type assignRoleNodeArg struct {
	NodeKey    string   `json:"node_key" jsonschema:"unique key of the plan node in this batch"`
	Role       string   `json:"role" jsonschema:"the team-declared role to resolve"`
	AvoidNodes []string `json:"avoid_nodes,omitempty" jsonschema:"other node_keys whose resolved agent must be avoided (Review≠Dev)"`
}

type assignRolesArgs struct {
	TeamID   string              `json:"team_id" jsonschema:"the team whose roster to resolve against"`
	Strategy string              `json:"strategy,omitempty" jsonschema:"least_busy (default) or round_robin"`
	Requests []assignRoleNodeArg `json:"requests" jsonschema:"the node→role resolution requests"`
}

func makeAssignRoles(cfg Config) mcp.ToolHandlerFor[assignRolesArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args assignRolesArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "assign_roles", map[string]any{
			"agent_id": cfg.AgentID, "team_id": args.TeamID, "strategy": args.Strategy, "requests": args.Requests,
		})
	}
}
