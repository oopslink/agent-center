// Team WebUI (Phase-1) — typed data layer.
//
// Types mirror the backend view structs (internal/team + internal/admin/api
// agent_tools_team.go): TeamView / RoleView / MemberView, the template export
// envelope, and the extract scrub findings. A handful of FE-only display fields
// (status, glyph, *_count, created, and the curation risk/loc enrichment) are
// added on top — the backend does not yet carry them, so Phase-1 supplies them
// from fixtures. See teamsFixtures.ts for the WHY and the migration path.
//
// Every hook is a react-query hook over the in-memory fixture store; when a real
// `/api/orgs/{slug}/teams` facade lands, only the queryFn/mutationFn bodies here
// change — call sites and types stay put.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { currentOrgScope } from './queryKeys';
import {
  cloneTeamsValue as clone,
  teamsStore,
  type TeamProjectLink,
} from './teamsFixtures';

// ---------------------------------------------------------------------------
// Types (backend-shaped + Phase-1 display extras)
// ---------------------------------------------------------------------------

/** RoleView — a declared role slot. `count` is present on templates/instances. */
export interface RoleView {
  role: string;
  cli: string;
  model: string;
  capability_tags: string[];
  max_concurrency: number;
  count?: number;
}

/** FE-only lifecycle badge (the backend has no team status column yet). */
export type TeamStatus = 'active' | 'draft';

/** TeamView — the create_team / get_team / list_teams response shape. */
export interface TeamView {
  id: string;
  org_id: string;
  name: string;
  description: string;
  roles: RoleView[];
  version: number;
  // Phase-1 display extras:
  glyph: string;
  status: TeamStatus;
  members_count: number;
  projects_count: number;
  created: string;
}

/** MemberView — add_member response + the Members tab rows. */
export interface MemberView {
  team_id: string;
  member_ref: string; // "agent:<id>" | "user:<id>"
  kind: 'agent' | 'human';
  role: string;
  // Phase-1 display extras:
  name: string;
  tags: string[];
  cli: string;
  model: string;
  concurrency: string;
  exclusive: boolean;
}

/** A role slot on a template — RoleView + a human-readable description. */
export interface RoleSlot extends RoleView {
  count: number;
  description?: string;
}

/** TeamTemplate — the template view (Phase-1: client-held, no server catalog). */
export interface TeamTemplate {
  id: string;
  org_id: string;
  name: string;
  description: string;
  roles: RoleSlot[];
  workflow_template_ref: string;
  curated: boolean;
  // Phase-1 display extras:
  source: string;
  source_kind: 'extract' | 'manual' | 'import';
  version_label: string;
  instances_count: number;
}

/** A team-memory index node — a doc slug, a pinned index, or a group label. */
export interface MemoryIndexEntry {
  slug?: string;
  pinned?: boolean;
  group?: string;
}

/** A rendered team-memory document (Phase-1: read-only). */
export interface MemoryDoc {
  slug: string;
  path: string;
  title: string;
  frontmatter: string | null;
  body: string;
}

/** Curation finding kind — mirrors internal/team ScrubKind. */
export type ScrubKind = 'code_name' | 'path' | 'url' | 'repo_name';
/** Curation action chosen per finding. */
export type ScrubAction = 'scrub' | 'keep';

/**
 * ScrubFinding — extract_from_team surfaces `{experience_slug, kind, token}`.
 * The risk/loc/reason/default_action fields are Phase-1 UI enrichment that the
 * mockup's curation gate renders; the backend does not encode them yet.
 */
export interface ScrubFinding {
  experience_slug: string;
  kind: ScrubKind;
  token: string;
  risk: 'hi' | 'md' | 'lo';
  loc: string;
  reason: string;
  default_action: ScrubAction;
}

export interface DirectoryAgent {
  name: string;
  status: 'working' | 'idle';
  role: string;
  teams: string[];
  model: string;
  load: number;
  backlog: number;
  last: string;
}

export interface DirectoryHuman {
  name: string;
  role: string;
  status: 'Joined' | 'Invited';
  email: string;
  created: string;
  last: string;
  teams: string[];
}

export type { TeamProjectLink };

/** A single role-slot input for create / instantiate builders. */
export interface RoleInput {
  role: string;
  cli: string;
  model: string;
  max_concurrency: number;
  count: number;
  tags: string;
  description?: string;
}

export const CLIS = ['claude-code', 'codex', 'gemini-cli'] as const;
export const MODELS = ['opus-4.8', 'sonnet-5', 'haiku-4.5', 'gpt-5'] as const;

/** Role → accent color (data-driven; inline style, not a Tailwind red utility). */
export const ROLE_COLORS: Record<string, string> = {
  planner: '#7C3AED',
  coder: '#3B82F6',
  reviewer: '#D97706',
  researcher: '#9333EA',
  ops: '#DC2626',
  designer: '#5CB198',
};

export const ROLE_DESC: Record<string, string> = {
  planner: '拆解需求、产出实现计划与架构取舍',
  coder: '实现功能、编写测试',
  reviewer: '评审正确性/安全、把阻塞位',
  ops: 'CI/CD、部署与回滚',
  researcher: '调研、数据回收与分析',
  designer: 'UX/UI 设计与原型',
};

export function roleColor(role: string): string {
  return ROLE_COLORS[role] || '#8b8794';
}

// ---------------------------------------------------------------------------
// Query keys (org-scoped, matching the queryKeys.ts convention)
// ---------------------------------------------------------------------------

function key(...parts: readonly unknown[]): readonly unknown[] {
  return ['org', currentOrgScope(), 'teams', ...parts];
}

export const teamKeys = {
  list: () => key('list'),
  detail: (id: string) => key('detail', id),
  members: (id: string) => key('members', id),
  projects: (id: string) => key('projects', id),
  memoryIndex: (id: string) => key('memory', id),
  memoryDoc: (id: string, slug: string) => key('memory', id, slug),
  templates: () => key('templates'),
  template: (id: string) => key('template', id),
  templateInstances: (id: string) => key('template', id, 'instances'),
  scrub: (id: string) => key('scrub', id),
  directoryAgents: () => key('directory', 'agents'),
  directoryHumans: () => key('directory', 'humans'),
};

// Resolve a fixture value on a microtask so hooks exercise the real loading path.
function resolve<T>(value: T): Promise<T> {
  return Promise.resolve(clone(value));
}

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------

export function useTeams() {
  return useQuery({ queryKey: teamKeys.list(), queryFn: () => resolve(teamsStore().teams) });
}

export function useTeam(id: string) {
  return useQuery({
    queryKey: teamKeys.detail(id),
    queryFn: () => {
      const t = teamsStore().teams.find((x) => x.id === id);
      if (!t) throw new Error('team_not_found');
      return resolve(t);
    },
    enabled: !!id,
  });
}

export interface CreateTeamInput {
  name: string;
  description: string;
  visibility: string;
  roles: RoleInput[];
}

export function useCreateTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTeamInput) => {
      const s = teamsStore();
      const id = `team-${(s.teams.length + 1).toString(16).padStart(6, '0')}`;
      const team: TeamView = {
        id,
        org_id: 'org-ooo',
        name: input.name,
        description: input.description,
        version: 1,
        glyph: input.name.slice(0, 2).toUpperCase(),
        status: 'active',
        members_count: 0,
        projects_count: 0,
        created: '刚刚',
        roles: input.roles.map((r) => ({
          role: r.role,
          cli: r.cli,
          model: r.model,
          max_concurrency: r.max_concurrency,
          count: r.count,
          capability_tags: r.tags ? r.tags.split(',').map((x) => x.trim()).filter(Boolean) : [],
        })),
      };
      s.teams.push(team);
      s.members[id] = [];
      s.projects[id] = [];
      return resolve(team);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.list() }),
  });
}

export function useDeleteTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => {
      const s = teamsStore();
      s.teams = s.teams.filter((t) => t.id !== id);
      delete s.members[id];
      delete s.projects[id];
      return resolve({ ok: true, team_id: id });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.list() }),
  });
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

export function useTeamMembers(id: string) {
  return useQuery({
    queryKey: teamKeys.members(id),
    queryFn: () => resolve(teamsStore().members[id] ?? []),
    enabled: !!id,
  });
}

export interface AddMemberInput {
  team_id: string;
  member_ref: string;
  name: string;
  kind: 'agent' | 'human';
  role: string;
  migrateFrom?: string;
}

export function useAddMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: AddMemberInput) => {
      const s = teamsStore();
      const list = s.members[input.team_id] ?? (s.members[input.team_id] = []);
      const member: MemberView = {
        team_id: input.team_id,
        member_ref: input.member_ref,
        kind: input.kind,
        role: input.role,
        name: input.name,
        tags: [],
        cli: input.kind === 'agent' ? 'claude-code' : '—',
        model: input.kind === 'agent' ? 'sonnet-5' : '—',
        concurrency: input.kind === 'agent' ? '0/2' : '—',
        exclusive: input.kind === 'agent',
      };
      list.push(member);
      const team = s.teams.find((t) => t.id === input.team_id);
      if (team) team.members_count = list.length;
      return resolve(member);
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.members(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.list() });
    },
  });
}

export function useRemoveMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; member_ref: string }) => {
      const s = teamsStore();
      const list = s.members[v.team_id] ?? [];
      s.members[v.team_id] = list.filter((m) => m.member_ref !== v.member_ref);
      const team = s.teams.find((t) => t.id === v.team_id);
      if (team) team.members_count = s.members[v.team_id].length;
      return resolve({ ok: true, ...v });
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.members(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.list() });
    },
  });
}

// ---------------------------------------------------------------------------
// Project association
// ---------------------------------------------------------------------------

export function useTeamProjects(id: string) {
  return useQuery({
    queryKey: teamKeys.projects(id),
    queryFn: () => resolve(teamsStore().projects[id] ?? []),
    enabled: !!id,
  });
}

export function useAssociateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; project_id: string; name: string }) => {
      const s = teamsStore();
      const list = s.projects[v.team_id] ?? (s.projects[v.team_id] = []);
      const link: TeamProjectLink = {
        team_id: v.team_id,
        project_id: v.project_id,
        name: v.name,
        glyph: v.name.slice(0, 2).toUpperCase(),
        repo: `repo: ${v.name}`,
        relation: list.length === 0 ? 'primary' : 'linked',
      };
      list.push(link);
      const team = s.teams.find((t) => t.id === v.team_id);
      if (team) team.projects_count = list.length;
      return resolve(link);
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.projects(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
    },
  });
}

export function useDisassociateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; project_id: string }) => {
      const s = teamsStore();
      const list = s.projects[v.team_id] ?? [];
      s.projects[v.team_id] = list.filter((p) => p.project_id !== v.project_id);
      const team = s.teams.find((t) => t.id === v.team_id);
      if (team) team.projects_count = s.projects[v.team_id].length;
      return resolve({ ok: true, ...v });
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.projects(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
    },
  });
}

// ---------------------------------------------------------------------------
// Team memory (read-only)
// ---------------------------------------------------------------------------

export function useTeamMemoryIndex(id: string) {
  return useQuery({
    queryKey: teamKeys.memoryIndex(id),
    queryFn: () => resolve(teamsStore().memoryIndex),
    enabled: !!id,
  });
}

export function useTeamMemoryDoc(id: string, slug: string) {
  return useQuery({
    queryKey: teamKeys.memoryDoc(id, slug),
    queryFn: () => {
      const doc = teamsStore().memoryDocs[slug];
      if (!doc) throw new Error('memory_not_found');
      return resolve(doc);
    },
    enabled: !!id && !!slug,
  });
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

export function useTeamTemplates() {
  return useQuery({ queryKey: teamKeys.templates(), queryFn: () => resolve(teamsStore().templates) });
}

export function useTeamTemplate(id: string) {
  return useQuery({
    queryKey: teamKeys.template(id),
    queryFn: () => {
      const t = teamsStore().templates.find((x) => x.id === id);
      if (!t) throw new Error('template_not_found');
      return resolve(t);
    },
    enabled: !!id,
  });
}

export function useTemplateInstances(id: string) {
  return useQuery({
    queryKey: teamKeys.templateInstances(id),
    queryFn: () => resolve(teamsStore().templateInstances[id] ?? []),
    enabled: !!id,
  });
}

export function useTemplateScrub(_templateId: string) {
  return useQuery({
    queryKey: teamKeys.scrub(_templateId),
    queryFn: () => resolve(teamsStore().scrub),
  });
}

/** extract_from_team — scan a source team for a curation draft. */
export function useExtractScrub(teamId: string) {
  return useQuery({
    queryKey: [...teamKeys.scrub(teamId), 'extract'],
    queryFn: () => resolve(teamsStore().scrub),
    enabled: !!teamId,
  });
}

export function useInstantiateTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { template_id: string; team_name: string; roles: RoleInput[] }) => {
      const s = teamsStore();
      const id = `team-${(s.teams.length + 1).toString(16).padStart(6, '0')}`;
      const team: TeamView = {
        id,
        org_id: 'org-ooo',
        name: input.team_name,
        description: '从模版实例化。',
        version: 1,
        glyph: input.team_name.slice(0, 2).toUpperCase(),
        status: 'active',
        members_count: 0,
        projects_count: 0,
        created: '刚刚',
        roles: input.roles.map((r) => ({
          role: r.role,
          cli: r.cli,
          model: r.model,
          max_concurrency: r.max_concurrency,
          count: r.count,
          capability_tags: r.tags ? r.tags.split(',').map((x) => x.trim()).filter(Boolean) : [],
        })),
      };
      s.teams.push(team);
      s.members[id] = [];
      s.projects[id] = [];
      const inst = s.templateInstances[input.template_id] ?? (s.templateInstances[input.template_id] = []);
      inst.push({ id, name: team.name });
      const tmpl = s.templates.find((x) => x.id === input.template_id);
      if (tmpl) tmpl.instances_count = inst.length;
      return resolve(team);
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.list() });
      qc.invalidateQueries({ queryKey: teamKeys.templateInstances(v.template_id) });
    },
  });
}

export interface SaveTemplateInput {
  name: string;
  description: string;
  source: string;
  source_kind: 'extract' | 'manual' | 'import';
  roles: RoleSlot[];
}

/** create_team_template — persist the curated draft (Phase-1: in-memory). */
export function useSaveTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SaveTemplateInput) => {
      const s = teamsStore();
      const id = `tmpl-${(s.templates.length + 1).toString(16)}`;
      const tmpl: TeamTemplate = {
        id,
        org_id: 'org-ooo',
        name: input.name,
        description: input.description,
        roles: input.roles,
        workflow_template_ref: 'plan-builtin',
        curated: true,
        source: input.source,
        source_kind: input.source_kind,
        version_label: 'v1 · curated',
        instances_count: 0,
      };
      s.templates.push(tmpl);
      s.templateInstances[id] = [];
      return resolve(tmpl);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.templates() }),
  });
}

/** export_team_template — the portable JSON envelope (team-template/v1). */
export function exportTemplateEnvelope(t: TeamTemplate): unknown {
  return {
    format: 'team-template/v1',
    name: t.name,
    description: t.description,
    roles: t.roles.map((r) => ({
      role: r.role,
      cli: r.cli,
      model: r.model,
      capability_tags: r.capability_tags,
      max_concurrency: r.max_concurrency,
      count: r.count,
    })),
    workflow_template_ref: t.workflow_template_ref,
    source_org_id: t.org_id,
    source_id: t.id,
  };
}

/** import_team_template — re-home an exported envelope into this org. */
export function useImportTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (doc: {
      name?: string;
      description?: string;
      roles?: Array<Partial<RoleSlot>>;
      workflow_template_ref?: string;
    }) => {
      const s = teamsStore();
      const id = `tmpl-${(s.templates.length + 1).toString(16)}`;
      const tmpl: TeamTemplate = {
        id,
        org_id: 'org-ooo',
        name: doc.name || 'imported-template',
        description: doc.description || '',
        roles: (doc.roles ?? []).map((r) => ({
          role: r.role || 'coder',
          cli: r.cli || 'claude-code',
          model: r.model || 'sonnet-5',
          capability_tags: r.capability_tags ?? [],
          max_concurrency: r.max_concurrency ?? 1,
          count: r.count ?? 1,
          description: r.description,
        })),
        workflow_template_ref: doc.workflow_template_ref || 'plan-builtin',
        curated: false,
        source: '导入 · cross-org JSON',
        source_kind: 'import',
        version_label: 'v1',
        instances_count: 0,
      };
      s.templates.push(tmpl);
      s.templateInstances[id] = [];
      return resolve(tmpl);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.templates() }),
  });
}

// ---------------------------------------------------------------------------
// Directory (agents / humans with team membership)
// ---------------------------------------------------------------------------

export function useDirectoryAgents() {
  return useQuery({ queryKey: teamKeys.directoryAgents(), queryFn: () => resolve(teamsStore().agents) });
}

export function useDirectoryHumans() {
  return useQuery({ queryKey: teamKeys.directoryHumans(), queryFn: () => resolve(teamsStore().humans) });
}
