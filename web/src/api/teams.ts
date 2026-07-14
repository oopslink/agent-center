// Team WebUI (Phase-1) — typed data layer.
//
// Types mirror the backend view structs (internal/team + internal/admin/api
// agent_tools_team.go): TeamView / RoleView / MemberView, the template export
// envelope, and the extract scrub findings. A handful of FE-only display fields
// (status, glyph, *_count, created, and the curation risk/loc enrichment) are
// added on top — the backend does not carry them, so the FE derives them (the
// scrub risk/loc/reason enrichment is display-only, layered over the truthful
// {experience_slug, kind, token} the facade returns; see enrichScrubFinding).
//
// Every hook now fetches through the real `/api/orgs/{slug}/…` facade
// (internal/webconsole/api/handlers_teams*.go); teamsFixtures.ts survives only as
// the MSW test backend (src/mocks/teamHandlers.ts), never the dev/prod runtime.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope } from './queryKeys';
import { type TeamProjectLink } from './teamsFixtures';

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
  ref: string; // canonical "agent:<identityID>" — use verbatim as a member_ref
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
  ref: string; // canonical "user:<identityID>" — use verbatim as a member_ref
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

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------

export function useTeams() {
  return useQuery({ queryKey: teamKeys.list(), queryFn: () => api.get<TeamView[]>('/teams') });
}

export function useTeam(id: string) {
  return useQuery({
    queryKey: teamKeys.detail(id),
    queryFn: () => api.get<TeamView>(`/teams/${id}`),
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
    mutationFn: (input: CreateTeamInput) => api.post<TeamView>('/teams', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.list() }),
  });
}

export function useDeleteTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/teams/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.list() }),
  });
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

export function useTeamMembers(id: string) {
  return useQuery({
    queryKey: teamKeys.members(id),
    queryFn: () => api.get<MemberView[]>(`/teams/${id}/members`),
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
    // Wire field is snake_case `migrate_from` (matches member_ref/team_id) — a
    // non-empty value routes the backend to the atomic MoveMember (leave old team
    // + join this one), bypassing the one-team exclusivity 409.
    mutationFn: (input: AddMemberInput) =>
      api.post<MemberView>(`/teams/${input.team_id}/members`, {
        member_ref: input.member_ref,
        name: input.name,
        kind: input.kind,
        role: input.role,
        migrate_from: input.migrateFrom,
      }),
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.members(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.list() });
      // A migration changes the agent's team membership → the directory pickers
      // (which show each agent's current team) must refetch.
      qc.invalidateQueries({ queryKey: teamKeys.directoryAgents() });
      qc.invalidateQueries({ queryKey: teamKeys.directoryHumans() });
    },
  });
}

export function useRemoveMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; member_ref: string }) =>
      api.del(`/teams/${v.team_id}/members/${encodeURIComponent(v.member_ref)}`),
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
    queryFn: () => api.get<TeamProjectLink[]>(`/teams/${id}/projects`),
    enabled: !!id,
  });
}

export function useAssociateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; project_id: string; name: string }) =>
      api.post<TeamProjectLink>(`/teams/${v.team_id}/projects`, v),
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: teamKeys.projects(v.team_id) });
      qc.invalidateQueries({ queryKey: teamKeys.detail(v.team_id) });
    },
  });
}

export function useDisassociateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; project_id: string }) =>
      api.del(`/teams/${v.team_id}/projects/${v.project_id}`),
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
    queryFn: () => api.get<MemoryIndexEntry[]>(`/teams/${id}/memory`),
    enabled: !!id,
  });
}

export function useTeamMemoryDoc(id: string, slug: string) {
  return useQuery({
    queryKey: teamKeys.memoryDoc(id, slug),
    queryFn: () => api.get<MemoryDoc>(`/teams/${id}/memory/${encodeURIComponent(slug)}`),
    enabled: !!id && !!slug,
  });
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

export function useTeamTemplates() {
  return useQuery({ queryKey: teamKeys.templates(), queryFn: () => api.get<TeamTemplate[]>('/team-templates') });
}

export function useTeamTemplate(id: string) {
  return useQuery({
    queryKey: teamKeys.template(id),
    queryFn: () => api.get<TeamTemplate>(`/team-templates/${id}`),
    enabled: !!id,
  });
}

export function useTemplateInstances(id: string) {
  return useQuery({
    queryKey: teamKeys.templateInstances(id),
    queryFn: () => api.get<TeamView[]>(`/team-templates/${id}/instances`),
    enabled: !!id,
  });
}

/** template curation scrub — the truthful {experience_slug, kind, token} findings
 *  from the template's seed memory (GET /team-templates/{tid}/scrub). Symmetric
 *  with useExtractScrub: the backend gives only truthful tokens, the FE layers the
 *  display-only risk/loc/reason/default_action enrichment on top. */
export function useTemplateScrub(templateId: string) {
  return useQuery({
    queryKey: teamKeys.scrub(templateId),
    queryFn: async () => {
      const res = await api.get<{
        scrub_findings: Array<{ experience_slug: string; kind: ScrubKind; token: string }>;
      }>(`/team-templates/${templateId}/scrub`);
      return res.scrub_findings.map(enrichScrubFinding);
    },
    enabled: !!templateId,
  });
}

// FE curation enrichment. The extract facade returns only the truthful
// {experience_slug, kind, token}; the mockup's curation gate renders
// risk/loc/reason/default_action. Risk is deliberately CAUTIOUS — a
// leak-prevention gate errs toward scrubbing, so identifiers and paths are high,
// links medium; every finding defaults to `scrub` (the reviewer opts to keep).
const SCRUB_RISK: Record<ScrubKind, ScrubFinding['risk']> = {
  code_name: 'hi',
  repo_name: 'hi',
  path: 'hi',
  url: 'md',
};
const SCRUB_REASON: Record<ScrubKind, string> = {
  code_name: '疑似内部代号/标识',
  repo_name: '疑似内部仓库名',
  path: '疑似内部路径/主机名',
  url: '疑似内部链接',
};
function enrichScrubFinding(f: { experience_slug: string; kind: ScrubKind; token: string }): ScrubFinding {
  return {
    experience_slug: f.experience_slug,
    kind: f.kind,
    token: f.token,
    risk: SCRUB_RISK[f.kind] ?? 'md',
    loc: f.experience_slug,
    reason: SCRUB_REASON[f.kind] ?? '疑似敏感 token',
    default_action: 'scrub',
  };
}

/** extract_from_team — scan a source team for a curation draft. The facade wraps
 *  the truthful findings under `.scrub_findings` ({experience_slug, kind, token});
 *  the risk/loc/reason/default_action columns are FE curation enrichment. */
export function useExtractScrub(teamId: string) {
  return useQuery({
    queryKey: [...teamKeys.scrub(teamId), 'extract'],
    queryFn: async () => {
      const res = await api.get<{
        scrub_findings: Array<{ experience_slug: string; kind: ScrubKind; token: string }>;
      }>(`/teams/${teamId}/extract`);
      return res.scrub_findings.map(enrichScrubFinding);
    },
    enabled: !!teamId,
  });
}

export function useInstantiateTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { template_id: string; team_name: string; roles: RoleInput[] }) =>
      api.post<TeamView>('/teams/instantiate', input),
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

/** create_team_template — persist the curated draft (POST /team-templates/save). */
export function useSaveTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SaveTemplateInput) =>
      api.post<TeamTemplate>('/team-templates/save', input),
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

/** import_team_template — re-home an exported envelope into this org
 *  (POST /team-templates/import). The backend applies the same field defaults
 *  the fixture path used (role→coder, cli→claude-code, model→sonnet-5, etc). */
export function useImportTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (doc: {
      name?: string;
      description?: string;
      roles?: Array<Partial<RoleSlot>>;
      workflow_template_ref?: string;
    }) => api.post<TeamTemplate>('/team-templates/import', doc),
    onSuccess: () => qc.invalidateQueries({ queryKey: teamKeys.templates() }),
  });
}

// ---------------------------------------------------------------------------
// Directory (agents / humans with team membership)
// ---------------------------------------------------------------------------

export function useDirectoryAgents() {
  return useQuery({
    queryKey: teamKeys.directoryAgents(),
    queryFn: () => api.get<DirectoryAgent[]>('/directory/agents'),
  });
}

export function useDirectoryHumans() {
  return useQuery({
    queryKey: teamKeys.directoryHumans(),
    queryFn: () => api.get<DirectoryHuman[]>('/directory/humans'),
  });
}
