import { http, HttpResponse } from 'msw';
import { teamsStore } from '@/api/teamsFixtures';
import type {
  MemberView,
  TeamProjectLink,
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
        role?: string;
        roles?: string[];
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
      const roles = Array.from(new Set((input.roles?.length ? input.roles : [input.role ?? '']).filter(Boolean)));
      const role = roles[0] ?? '';
      const existing = list.find((m) => m.member_ref === input.member_ref);
      if (existing) {
        existing.roles = Array.from(new Set([...(existing.roles ?? [existing.role]), ...roles]));
        existing.role = existing.roles.join(', ');
        return json(existing, 201);
      }
      const member: MemberView = {
        team_id: id,
        member_ref: input.member_ref,
        kind: input.kind,
        role: roles.join(', ') || role,
        roles,
        name: input.name,
        tags: [],
        cli: input.kind === 'agent' ? 'claude-code' : '—',
        model: input.kind === 'agent' ? 'sonnet-5' : '—',
        concurrency: input.kind === 'agent' ? '2' : '—',
        exclusive: false,
      };
      list.push(member);
      const team = s.teams.find((t) => t.id === id);
      if (team) team.members_count = new Set(list.map((m) => m.member_ref)).size;
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

    // ---- P2: team memory ----
    http.get('/api/teams/:id/memory', () => json(teamsStore().memoryIndex)),

    http.get('/api/teams/:id/memory/:entry', ({ params }) => {
      const entry = decodeURIComponent(String(params.entry));
      const doc = teamsStore().memoryDocs[entry] ?? teamsStore().memoryDocs[String(params.entry)];
      return doc
        ? json(doc)
        : HttpResponse.json({ error: 'not_found', message: 'memory_not_found' }, { status: 404 });
    }),

    // ---- P3: directory (agents / humans with team membership) ----
    http.get('/api/directory/agents', () => json(teamsStore().agents)),
    http.get('/api/directory/humans', () => json(teamsStore().humans)),
  ];
}
