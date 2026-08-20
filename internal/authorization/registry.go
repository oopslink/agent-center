package authorization

import (
	"strings"
)

var definitions = []PermissionDefinition{
	def("org.read", []string{"org"}, []string{"read"}, []string{"members"}),
	def("org.settings.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("org.lifecycle.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("org.member.list", []string{"org"}, []string{"list"}, []string{"members"}),
	def("org.member.create.human", []string{"org"}, []string{"create"}, []string{"members.role"}),
	def("org.member.create.agent", []string{"org"}, []string{"create"}, []string{"members.role"}),
	def("org.member.role.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("org.member.disable", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("org.invitation.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("org.analytics.read", []string{"org"}, []string{"read"}, []string{"members.role"}),
	def("org.work_items.read", []string{"org"}, []string{"read"}, []string{"members"}),
	def("project.read", []string{"project"}, []string{"read"}, []string{"pm_project_members"}),
	def("project.write", []string{"project"}, []string{"update"}, []string{"pm_project_members"}),
	def("project.member.add", []string{"project"}, []string{"create"}, []string{"pm_project_members"}),
	def("project.member.remove", []string{"project"}, []string{"delete"}, []string{"pm_project_members.role"}),
	def("project.repo_ref.manage", []string{"project"}, []string{"manage"}, []string{"pm_project_members"}),
	def("project.stage.manage", []string{"project"}, []string{"manage"}, []string{"pm_project_members.role"}),
	def("team.read", []string{"team"}, []string{"read"}, []string{"members"}),
	def("team.create", []string{"org"}, []string{"create"}, []string{"members"}),
	def("team.write", []string{"team"}, []string{"update"}, []string{"members"}),
	def("team.member.manage", []string{"team"}, []string{"manage"}, []string{"members"}),
	def("team.project.link.manage", []string{"team"}, []string{"manage"}, []string{"members"}),
	def("team.runtime_config.manage", []string{"team"}, []string{"manage"}, []string{"members"}),
	def("team.memory.read", []string{"team"}, []string{"read"}, []string{"members", "team_members"}),
	def("team.memory.propose", []string{"team"}, []string{"create"}, []string{"members.role", "team_members"}),
	def("team.memory.review", []string{"team"}, []string{"review"}, []string{"members.role", "team_memory_policy_curators"}),
	def("team.git.read", []string{"team"}, []string{"read"}, []string{"team_members"}),
	def("team.git.write", []string{"team"}, []string{"update"}, []string{"team_members"}),
	def("conversation.read", []string{"conversation"}, []string{"read"}, []string{"conversations.participants"}),
	def("conversation.post", []string{"conversation"}, []string{"create"}, []string{"conversations.participants"}),
	def("file.upload", []string{"file"}, []string{"upload"}, []string{"file_references"}),
	def("file.download", []string{"file"}, []string{"download"}, []string{"file_references"}),
	def("file.attach", []string{"file"}, []string{"attach"}, []string{"file_references"}),
	def("agent.operate.self", []string{"agent"}, []string{"manage"}, []string{"agents.worker_id"}),
	def("worker.capability.report", []string{"worker"}, []string{"report"}, []string{"admin_tokens.owner"}),
	def("worker.heartbeat", []string{"worker"}, []string{"heartbeat"}, []string{"admin_tokens.owner"}),
	def("worker.enroll", []string{"worker"}, []string{"create"}, []string{"admin_tokens.scopes_json"}),
	def("dispatch.pull", []string{"worker"}, []string{"pull"}, []string{"admin_tokens.scopes_json"}),
	def("task.internal.report", []string{"task"}, []string{"report"}, []string{"admin_tokens.scopes_json"}),
	def("task.read", []string{"task"}, []string{"read"}, []string{"pm_project_members", "pm_tasks.assignee"}),
	def("task.write", []string{"task"}, []string{"update"}, []string{"pm_project_members"}),
	def("task.start.self", []string{"task"}, []string{"start"}, []string{"pm_tasks.assignee"}),
	def("task.heartbeat.self", []string{"task"}, []string{"heartbeat"}, []string{"pm_tasks.assignee"}),
	def("task.complete.self", []string{"task"}, []string{"complete"}, []string{"pm_tasks.assignee"}),
	def("task.block.self", []string{"task"}, []string{"block"}, []string{"pm_tasks.assignee"}),
	def("issue.read", []string{"issue"}, []string{"read"}, []string{"pm_project_members"}),
	def("issue.write", []string{"issue"}, []string{"update"}, []string{"pm_project_members"}),
	def("plan.read", []string{"plan"}, []string{"read"}, []string{"pm_project_members"}),
	def("plan.write", []string{"plan"}, []string{"update"}, []string{"pm_project_members"}),
	def("template.read", []string{"org"}, []string{"read"}, []string{"members"}),
	def("template.write", []string{"org"}, []string{"update"}, []string{"members"}),
	def("coderepo.workspace.read", []string{"org"}, []string{"read"}, []string{"members"}),
	def("coderepo.workspace.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("coderepo.project_ref.read", []string{"project"}, []string{"read"}, []string{"pm_project_members"}),
	def("ai_runtime.catalog.read", []string{"org"}, []string{"read"}, []string{"members"}),
	def("ai_runtime.catalog.export", []string{"org"}, []string{"export"}, []string{"members"}),
	def("ai_runtime.catalog.manage", []string{"org"}, []string{"manage"}, []string{"members.role"}),
	def("model_catalog.manage", []string{"org"}, []string{"manage"}, []string{"agent_worker_binding"}),
	def("secret.resolve", []string{"secret"}, []string{"read"}, []string{"admin_tokens.scopes_json"}),
	def("blob.put", []string{"blob"}, []string{"put"}, []string{"admin_tokens.scopes_json"}),
	def("admin_token.manage", []string{"admin_token"}, []string{"manage"}, []string{"admin_tokens.scopes_json"}),
	def("git.global.read", []string{"git"}, []string{"read"}, []string{"system"}),
	def("git.agent.read.self", []string{"agent"}, []string{"read"}, []string{"agents.identity_member_id"}),
	def("git.agent.write.self", []string{"agent"}, []string{"update"}, []string{"agents.identity_member_id"}),
	def("background.auto_assign", []string{"background"}, []string{"run"}, []string{"system.background"}),
	def("background.lease_check", []string{"background"}, []string{"run"}, []string{"system.background"}),
	def("background.overdue_block_reminder", []string{"background"}, []string{"run"}, []string{"system.background"}),
	def("background.plan_reconcile", []string{"background"}, []string{"run"}, []string{"system.background"}),
	def("background.resolved_issue_close", []string{"background"}, []string{"run"}, []string{"system.background"}),
}

func def(key string, kinds, actions, sources []string) PermissionDefinition {
	return PermissionDefinition{
		Key:           PermissionKey(key),
		Category:      "access",
		ResourceKinds: append([]string(nil), kinds...),
		Actions:       append([]string(nil), actions...),
		LegacySources: append([]string(nil), sources...),
	}
}

func Definitions() []PermissionDefinition {
	out := make([]PermissionDefinition, len(definitions))
	copy(out, definitions)
	return out
}

func Definition(key PermissionKey) (PermissionDefinition, bool) {
	for _, d := range definitions {
		if d.Key == key {
			return d, true
		}
	}
	return PermissionDefinition{}, false
}

func PermissionDefinedForResource(key PermissionKey, resourceKind string) bool {
	d, ok := Definition(key)
	if !ok {
		return false
	}
	resourceKind = strings.TrimSpace(resourceKind)
	for _, k := range d.ResourceKinds {
		if k == resourceKind {
			return true
		}
	}
	return false
}

func PermissionForBearerScope(scope string) (PermissionKey, bool) {
	switch strings.TrimSpace(scope) {
	case "*":
		return "*", true
	case "admin:token":
		return "admin_token.manage", true
	case "secret:resolve":
		return "secret.resolve", true
	case "blob:put":
		return "blob.put", true
	case "dispatch:pull":
		return "dispatch.pull", true
	case "task:*":
		return "task.internal.report", true
	case "workforce:enroll":
		return "worker.enroll", true
	}
	return "", false
}

func BearerScopeAllows(have []string, required string) bool {
	required = strings.TrimSpace(required)
	for _, scope := range have {
		scope = strings.TrimSpace(scope)
		if scope == "*" || scope == required {
			return true
		}
		if strings.HasSuffix(scope, ":*") {
			prefix := strings.TrimSuffix(scope, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}
