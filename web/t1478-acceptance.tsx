import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import Access from './src/pages/Access';
import { useAppStore } from './src/store/app';
import './src/index.css';

type Resource = { kind: string; id: string; org_id?: string; project_id?: string; label?: string };

const subjects = [
  { ref: 'user:hayang', kind: 'human', name: 'Hayang', role: 'owner', status: 'joined', team_names: ['agent-center core'] },
  { ref: 'user:ops', kind: 'human', name: 'Ops Reviewer', role: 'admin', status: 'joined', team_names: ['release'] },
  { ref: 'agent:builder', kind: 'agent', name: 'Builder', role: 'member', status: 'joined', team_names: ['agent-center core'] },
  { ref: 'agent:external', kind: 'agent', name: 'External Bot', role: 'member', status: 'unavailable', team_names: [] },
];

const catalog = [
  { key: 'org.read', label: 'Read organization', description: 'Open organization-scoped resources.', resource_kinds: ['org'], actions: ['read'], risk: 'low', category: 'access', legacy_sources: ['org_role'] },
  { key: 'org.member.role.manage', label: 'Manage org roles', description: 'Change owner/admin/member assignments.', resource_kinds: ['org'], actions: ['manage'], risk: 'high', high_risk: true, category: 'access', legacy_sources: ['org_role'] },
  { key: 'project.write', label: 'Write project', description: 'Create and update project work items.', resource_kinds: ['project'], actions: ['write'], risk: 'medium', category: 'access', legacy_sources: ['project_member'] },
  { key: 'team.memory.review', label: 'Review team memory', description: 'Promote or reject team memory proposals.', resource_kinds: ['team'], actions: ['review'], risk: 'high', high_risk: true, category: 'access', legacy_sources: ['org_role', 'team_memory_policy'] },
  { key: 'file.download', label: 'Download files', description: 'Download files reachable through live scope references.', resource_kinds: ['file', 'task', 'issue', 'plan', 'conversation'], actions: ['download'], risk: 'medium', category: 'access', legacy_sources: ['file_scope'] },
];

const roles = [
  { id: 'org:owner', name: 'Org owner', scope_kind: 'org', description: 'Full organization administration.', permissions: ['org.read', 'org.member.role.manage', 'team.memory.review'], editable: false, source: 'org_role', high_risk: true },
  { id: 'org:admin', name: 'Org admin', scope_kind: 'org', description: 'Operational administration without owner transfer.', permissions: ['org.read', 'project.write'], editable: true, source: 'org_role' },
  { id: 'team:curator', name: 'Team curator', scope_kind: 'team', description: 'Review team memory proposals when policy grants it.', permissions: ['team.memory.review'], editable: true, source: 'team_memory_policy', high_risk: true },
];

const grants = [
  { id: 'grant-custom-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' }, source: 'custom_role', status: 'expires_soon', starts_at: '2026-08-14T00:00:00Z', expires_at: '2026-09-21T00:00:00Z', created_by: 'user:hayang', created_at: '2026-08-14T00:00:00Z', revoked_at: null, role_id: 'role-access-project-write', risk: 'medium' },
  { id: 'grant-derived-owner', subject_ref: 'user:hayang', subject_name: 'Hayang', permission: 'org.member.role.manage', resource: { kind: 'org', id: 'org-test', label: 'Test Org' }, source: 'org_role', status: 'active', starts_at: '2026-08-14T00:00:00Z', expires_at: null, created_by: 'system', created_at: '2026-08-14T00:00:00Z', revoked_at: null, risk: 'high' },
];

const decisions = [
  { allowed: true, subject_ref: 'user:hayang', permission: 'org.read', resource: { kind: 'org', id: 'org-test', label: 'Test Org' }, source: 'org_role', reason: 'owner role derives org.read', evidence_ref: 'members:mem-1', status: 'allowed', risk: 'low' },
  { allowed: true, subject_ref: 'user:hayang', permission: 'org.member.role.manage', resource: { kind: 'org', id: 'org-test', label: 'Test Org' }, source: 'org_role', reason: 'owner role derives org.member.role.manage', evidence_ref: 'members:mem-1', status: 'allowed', risk: 'high' },
  { allowed: true, subject_ref: 'agent:builder', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' }, source: 'project_member', reason: 'project membership derives project.write', evidence_ref: 'pm_project_members:pmem-1', status: 'allowed', expires_at: '2026-09-21T00:00:00Z', grant_id: 'grant-custom-1', risk: 'medium' },
  { allowed: true, subject_ref: 'agent:builder', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' }, source: 'custom_role', reason: 'matched unified authorization service', evidence_ref: 'authorization_role_assignments:grant-custom-1', status: 'allowed', expires_at: '2026-09-21T00:00:00Z', grant_id: 'grant-custom-1', role_id: 'role-access-project-write', risk: 'medium' },
  { allowed: true, subject_ref: 'agent:builder', permission: 'team.memory.review', resource: { kind: 'team', id: 'team-7c19b0', org_id: 'org-test', label: 'agent-center core' }, source: 'team_role_ram', reason: 'matched team_role_ram', evidence_ref: 'team_role_ram_role_mappings:team-7c19b0/reviewer/team-curator', status: 'allowed', role_id: 'team-curator', risk: 'high' },
  { allowed: false, subject_ref: 'agent:builder', permission: 'team.memory.review', resource: { kind: 'team', id: 'team-core', org_id: 'org-test', label: 'agent-center core' }, source: 'team_memory_policy', reason: 'not a curator for this team policy', evidence_ref: 'team_memory_policy:team-core', status: 'denied', risk: 'high' },
  { allowed: false, subject_ref: 'agent:external', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' }, source: 'project_member', reason: 'subject is not a joined organization member', evidence_ref: 'members:missing', status: 'unauthorized', risk: 'medium' },
  { allowed: false, subject_ref: 'user:ops', permission: 'file.download', resource: { kind: 'team', id: 'team-core', org_id: 'org-test', label: 'agent-center core' }, source: 'file_scope', reason: 'file.download does not apply to team resources', evidence_ref: 'permission_registry:file.download', status: 'not_applicable', risk: 'medium' },
];

const teams = [
  { id: 'team-7c19b0', org_id: 'org-test', name: 'agent-center core', description: 'Core delivery team', roles: [{ role: 'reviewer', cli: 'codex', model: 'gpt-5', capability_tags: [], max_concurrency: 1 }], version: 7, glyph: 'AC', status: 'active', members_count: 1, projects_count: 1, created: '2026-08-14T00:00:00Z' },
];

const ramRoles = [
  { id: 'team-basic', stable_key: 'team-basic', name: 'Team basic', version: 1, kind: 'system', description: 'Read team metadata and memory.', permissions: ['team.read', 'team.memory.read'], risk: 'low', scope: 'team', references: 0 },
  { id: 'team-curator', stable_key: 'team-curator', name: 'Team curator', version: 2, kind: 'system', description: 'Review team memory.', permissions: ['team.read', 'team.write', 'team.memory.review'], risk: 'high', scope: 'team', references: 1 },
];

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function batchItems(body: { subject_refs?: string[]; permission_keys?: string[]; resources?: Resource[] }, applied: boolean) {
  const items: Array<Record<string, unknown>> = [];
  for (const subjectRef of body.subject_refs ?? []) {
    for (const permission of body.permission_keys ?? []) {
      for (const resource of body.resources ?? []) {
        const def = catalog.find((p) => p.key === permission);
        const subject = subjects.find((s) => s.ref === subjectRef);
        let status = 'allowed';
        let reason = applied ? 'grant applied by unified authorization API' : 'grant can be applied by the permission API';
        if (!subject || subject.status === 'unavailable') {
          status = 'unauthorized';
          reason = 'subject is unavailable or outside this organization';
        } else if (!def?.resource_kinds.includes(resource.kind as never)) {
          status = 'not_applicable';
          reason = `${permission} does not apply to ${resource.kind}`;
        } else if (permission === 'org.member.role.manage' && subject.kind === 'agent') {
          status = 'unauthorized';
          reason = 'agents cannot receive organization role-management grants';
        }
        items.push({ id: `item-${items.length + 1}`, subject_ref: subjectRef, subject_name: subject?.name ?? subjectRef, permission, resource, status, risk: def?.risk ?? 'medium', high_risk: def?.risk === 'high', reason, grant_id: status === 'allowed' ? `grant-new-${items.length + 1}` : undefined });
      }
    }
  }
  return items;
}

window.fetch = async (input, init) => {
  const url = new URL(typeof input === 'string' ? input : input.url, window.location.origin);
  const path = url.pathname.replace(/^\/api\/orgs\/test/, '').replace(/^\/api/, '');
  window.__t1478Requests = [...(window.__t1478Requests ?? []), { method: init?.method ?? 'GET', path, status: 200 }];
  if (path === '/auth/me') return json({ id: 'user-hayang', identity_id: 'user:hayang', display_name: 'Hayang', orgs: [{ id: 'org-test', slug: 'test', name: 'Test Org', role: 'owner' }] });
  if (path === '/auth/bootstrap') return json({ initialized: true });
  if (path === '/access/overview') {
    const filtered = decisions.filter((row) => {
      const status = url.searchParams.get('status');
      if (status && status !== 'all' && row.status !== status) return false;
      return true;
    });
    return json({ generated_at: '2026-08-22T07:30:00Z', subjects, roles, catalog, decisions: filtered, grants, summary: { allowed: filtered.filter((d) => d.status === 'allowed').length, high_risk: filtered.filter((d) => d.risk === 'high').length, expiring: 1, denied: filtered.filter((d) => d.status === 'denied' || d.status === 'unauthorized').length, not_applicable: filtered.filter((d) => d.status === 'not_applicable').length } });
  }
  if (path === '/access/ram-roles') return json({ roles: ramRoles });
  if (path.startsWith('/access/ram-roles/')) return json({ ...ramRoles[1], latest: ramRoles[1], versions: [ramRoles[1]], references: [{ team_id: 'team-7c19b0', team_name: 'agent-center core', team_role: 'reviewer' }] });
  if (path === '/teams') return json(teams);
  if (path === '/teams/team-7c19b0/members') return json([{ team_id: 'team-7c19b0', member_ref: 'agent:builder', kind: 'agent', role: 'reviewer', roles: ['reviewer'], name: 'Builder', tags: [], cli: 'codex', model: 'gpt-5', concurrency: '1', exclusive: false }]);
  if (path === '/teams/team-7c19b0/roles/reviewer/ram-roles') return json({ team_id: 'team-7c19b0', team_role: 'reviewer', ram_role_ids: ['team-curator'], version: 7 });
  if (path === '/permissions/effective') return json({ subject_ref: 'user:hayang', resource: { kind: 'org', id: 'org-test', org_id: 'org-test' }, permissions: [{ key: 'org.read', source: 'org_role' }, { key: 'org.member.role.manage', source: 'org_role' }] });
  if (path === '/permissions/audit') return json({ events: [{ id: 'audit-1', event_type: 'authorization.assignment.created', actor_ref: 'user:hayang', subject_ref: url.searchParams.get('subject_ref') ?? 'agent:builder', permission_key: 'project.write', resource_kind: 'project', resource_id: 'proj-a', role_id: 'role-access-project-write', assignment_id: 'grant-custom-1', payload: {}, created_at: '2026-08-14T04:00:00Z' }] });
  if (path === '/access/batch/preview') {
    const body = JSON.parse(String(init?.body ?? '{}'));
    const items = batchItems(body, false);
    return json({ request_id: 'preview-harness-1', expires_at: body.expires_at ?? null, items, summary: { total: items.length, grantable: items.filter((i) => i.status === 'allowed').length, high_risk: items.filter((i) => i.high_risk).length, unauthorized: items.filter((i) => i.status === 'unauthorized').length, not_applicable: items.filter((i) => i.status === 'not_applicable').length } });
  }
  if (path === '/access/batch/apply') {
    const body = JSON.parse(String(init?.body ?? '{}'));
    const items = batchItems(body, true);
    const failed = items.filter((i) => i.status !== 'allowed').length;
    return json({ operation_id: 'apply-harness-1', applied_at: '2026-08-22T07:31:00Z', items, summary: { total: items.length, succeeded: items.filter((i) => i.status === 'allowed').length, failed, unauthorized: items.filter((i) => i.status === 'unauthorized').length, not_applicable: items.filter((i) => i.status === 'not_applicable').length, partial_failure: failed > 0 } });
  }
  window.__t1478Requests = (window.__t1478Requests ?? []).map((entry, idx, arr) => idx === arr.length - 1 ? { ...entry, status: 404 } : entry);
  return json({ error: 'harness_unhandled', message: `Unhandled T1478 harness request: ${path}` }, 404);
};

declare global {
  interface Window {
    __t1478Requests?: Array<{ method: string; path: string; status: number }>;
  }
}

window.history.replaceState({}, '', '/organizations/test/access?view=subject-access');
useAppStore.setState({ currentUserId: 'user:hayang' });

const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Access />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
