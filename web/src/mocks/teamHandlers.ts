import { http, HttpResponse } from 'msw';
import { teamsStore } from '@/api/teamsFixtures';
import type {
  MemberView,
  RoleSlot,
  SaveTemplateInput,
  TeamProjectLink,
  TeamTemplate,
  TeamView,
} from '@/api/teams';

// MSW test doubles for the Phase-1 team facade (GET/POST/DELETE under /api/teams…).
//
// Backed by the mutable teamsFixtures store so the fixture seed doubles as the test
// backend: every teams.ts hook now fetches THROUGH these handlers (all 22 P1/P2/P3
// hooks swapped fixture→fetch), and each handler mutates the same store so a test's
// reads and writes stay consistent. Production hits the real Go facade
// (internal/webconsole/api/handlers_teams*.go); per the handlers.ts convention these
// run ONLY under vitest, never the dev runtime.
//
// Registered unscoped (/api/teams…); handlers.ts auto-derives the /api/orgs/:slug/…
// variant. In jsdom the org slug is null (MemoryRouter ≠ window.location), so
// requests land on the unscoped form.

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });
const notFound = () =>
  HttpResponse.json({ error: 'not_found', message: 'team_not_found' }, { status: 404 });

export function teamHandlers() {
  return [
    // ---- teams CRUD ----
    http.get('/api/teams', () => json(teamsStore().teams)),

    http.post('/api/teams', async ({ request }) => {
      const input = (await request.json()) as {
        name: string;
        description: string;
        roles: Array<{
          role: string;
          cli: string;
          model: string;
          max_concurrency: number;
          count?: number;
          tags?: string;
        }>;
      };
      const s = teamsStore();
      const id = `team-${(s.teams.length + 1).toString(16).padStart(6, '0')}`;
      const team: TeamView = {
        id,
        org_id: 'org-ooo',
        name: input.name,
        description: input.description,
        version: 1,
        glyph: input.name.slice(0, 2).toUpperCase(),
        status: 'draft', // fresh team has no members → draft (mirrors the facade)
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
      return json(team, 201);
    }),

    http.get('/api/teams/:id', ({ params }) => {
      const t = teamsStore().teams.find((x) => x.id === String(params.id));
      return t ? json(t) : notFound();
    }),

    http.delete('/api/teams/:id', ({ params }) => {
      const s = teamsStore();
      const id = String(params.id);
      s.teams = s.teams.filter((t) => t.id !== id);
      delete s.members[id];
      delete s.projects[id];
      return json({ ok: true });
    }),

    // ---- members ----
    http.get('/api/teams/:id/members', ({ params }) =>
      json(teamsStore().members[String(params.id)] ?? []),
    ),

    http.post('/api/teams/:id/members', async ({ params, request }) => {
      const input = (await request.json()) as {
        member_ref: string;
        name: string;
        kind: 'agent' | 'human';
        role: string;
        migrate_from?: string;
      };
      const s = teamsStore();
      const id = String(params.id);
      // migrate_from (the source team ID) → mirror the backend's atomic
      // MoveMember: leave the old team before joining this one, so the agent ends
      // up on exactly one team (no exclusivity 409).
      if (input.migrate_from) {
        const old = s.teams.find((t) => t.id === input.migrate_from);
        if (old) {
          const prev = s.members[old.id] ?? [];
          s.members[old.id] = prev.filter((m) => m.member_ref !== input.member_ref);
          old.members_count = s.members[old.id].length;
        }
      }
      const list = s.members[id] ?? (s.members[id] = []);
      const member: MemberView = {
        team_id: id,
        member_ref: input.member_ref,
        kind: input.kind,
        role: input.role,
        name: input.name,
        tags: [],
        cli: input.kind === 'agent' ? 'claude-code' : '—',
        model: input.kind === 'agent' ? 'sonnet-5' : '—',
        concurrency: input.kind === 'agent' ? '2' : '—',
        exclusive: false,
      };
      list.push(member);
      const team = s.teams.find((t) => t.id === id);
      if (team) team.members_count = list.length;
      return json(member, 201);
    }),

    http.delete('/api/teams/:id/members/:ref', ({ params }) => {
      const s = teamsStore();
      const id = String(params.id);
      const ref = String(params.ref);
      const list = s.members[id] ?? [];
      s.members[id] = list.filter((m) => m.member_ref !== ref);
      const team = s.teams.find((t) => t.id === id);
      if (team) team.members_count = s.members[id].length;
      return json({ ok: true });
    }),

    // ---- projects ----
    http.get('/api/teams/:id/projects', ({ params }) =>
      json(teamsStore().projects[String(params.id)] ?? []),
    ),

    http.post('/api/teams/:id/projects', async ({ params, request }) => {
      const input = (await request.json()) as { project_id: string; name: string };
      const s = teamsStore();
      const id = String(params.id);
      const list = s.projects[id] ?? (s.projects[id] = []);
      const link: TeamProjectLink = {
        team_id: id,
        project_id: input.project_id,
        name: input.name,
        glyph: input.name.slice(0, 2).toUpperCase(),
        repo: '',
        relation: list.length === 0 ? 'primary' : 'linked',
      };
      list.push(link);
      const team = s.teams.find((t) => t.id === id);
      if (team) team.projects_count = list.length;
      return json(link, 201);
    }),

    // disassociate — DELETE the Team↔Project link (inverse of the POST above).
    http.delete('/api/teams/:id/projects/:projectId', ({ params }) => {
      const s = teamsStore();
      const id = String(params.id);
      const projectId = String(params.projectId);
      const list = s.projects[id] ?? [];
      s.projects[id] = list.filter((p) => p.project_id !== projectId);
      const team = s.teams.find((t) => t.id === id);
      if (team) team.projects_count = s.projects[id].length;
      return json({ ok: true, team_id: id, project_id: projectId });
    }),

    // ---- P2: team memory (read-only) ----
    http.get('/api/teams/:id/memory', () => json(teamsStore().memoryIndex)),

    http.get('/api/teams/:id/memory/:entry', ({ params }) => {
      const doc = teamsStore().memoryDocs[String(params.entry)];
      return doc
        ? json(doc)
        : HttpResponse.json({ error: 'not_found', message: 'memory_not_found' }, { status: 404 });
    }),

    // ---- P2: templates (Phase-1 in-memory; list + get only — save/import are residual) ----
    http.get('/api/team-templates', () => json(teamsStore().templates)),

    http.get('/api/team-templates/:tid', ({ params }) => {
      const t = teamsStore().templates.find((x) => x.id === String(params.tid));
      return t
        ? json(t)
        : HttpResponse.json({ error: 'not_found', message: 'template_not_found' }, { status: 404 });
    }),

    // save — persist a CURATED template draft → TeamTemplate (201).
    http.post('/api/team-templates/save', async ({ request }) => {
      const input = (await request.json()) as SaveTemplateInput;
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
      return json(tmpl, 201);
    }),

    // import — re-home an exported envelope as an UN-curated template → 201.
    http.post('/api/team-templates/import', async ({ request }) => {
      const doc = (await request.json()) as {
        name?: string;
        description?: string;
        roles?: Array<Partial<RoleSlot>>;
        workflow_template_ref?: string;
      };
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
      return json(tmpl, 201);
    }),

    // instances — teams instantiated from a template → TeamView[] (the FE reads
    // id/name off each; the fixture holds those two fields per instance).
    http.get('/api/team-templates/:tid/instances', ({ params }) =>
      json(teamsStore().templateInstances[String(params.tid)] ?? []),
    ),

    // scrub — the template's curation findings stripped to the truthful 3 fields
    // (same {scrub_findings} envelope as /teams/:id/extract; FE enriches). Unknown
    // tid → 404. Backed by the shared seed scrub fixture (the store holds one
    // findings set; a real backend derives per-template from its seed memory).
    http.get('/api/team-templates/:tid/scrub', ({ params }) => {
      const s = teamsStore();
      const tid = String(params.tid);
      if (!s.templates.some((t) => t.id === tid)) {
        return HttpResponse.json(
          { error: 'not_found', message: 'template_not_found' },
          { status: 404 },
        );
      }
      const findings = s.scrub.map((f) => ({
        experience_slug: f.experience_slug,
        kind: f.kind,
        token: f.token,
      }));
      return json({ scrub_findings: findings });
    }),

    // ---- P2: extract — findings stripped to the truthful 3 fields (FE enriches) ----
    http.get('/api/teams/:id/extract', () =>
      json({
        draft: {},
        scrub_findings: teamsStore().scrub.map((f) => ({
          experience_slug: f.experience_slug,
          kind: f.kind,
          token: f.token,
        })),
        dropped_project: 0,
        curated: false,
      }),
    ),

    // ---- P2: instantiate (project-decoupled) ----
    http.post('/api/teams/instantiate', async ({ request }) => {
      const input = (await request.json()) as {
        template_id: string;
        team_name: string;
        roles: Array<{
          role: string;
          cli: string;
          model: string;
          max_concurrency: number;
          count?: number;
          tags?: string;
        }>;
      };
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
      const inst =
        s.templateInstances[input.template_id] ?? (s.templateInstances[input.template_id] = []);
      inst.push({ id, name: team.name });
      const tmpl = s.templates.find((x) => x.id === input.template_id);
      if (tmpl) tmpl.instances_count = inst.length;
      return json(team, 201);
    }),

    // ---- P3: directory (agents / humans with team membership) ----
    http.get('/api/directory/agents', () => json(teamsStore().agents)),
    http.get('/api/directory/humans', () => json(teamsStore().humans)),
  ];
}
