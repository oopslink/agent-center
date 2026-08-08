// Team WebUI (Phase-1) — in-memory fixture store.
//
// WHY fixtures and not the real backend: the Team domain is wired only on the
// `/admin/agent-tools/*` RPC surface (worker-token auth, agent_id bound to a
// worker) — it is NOT reachable from a browser session, and it exposes no
// list-members / member→teams endpoints, nor any team active/draft status. So a
// browser-facing Phase-1 UI cannot bind to it yet.
// This module backs the UI with typed fixtures shaped to the backend view
// structs (TeamView / RoleView / MemberView), so swapping to a future
// `/api/orgs/{slug}/teams` REST facade is a queryFn-only change in teams.ts.
// Mutations mutate this store so create / member flows feel live within a
// session.
//
// The data mirrors the finalised v7 mockup (team-webui-mockup.html).

import type {
  DirectoryAgent,
  DirectoryHuman,
  MemberView,
  MemoryDoc,
  MemoryIndexEntry,
  TeamView,
} from './teams';

export interface TeamsData {
  teams: TeamView[];
  members: Record<string, MemberView[]>;
  projects: Record<string, TeamProjectLink[]>;
  memoryIndex: MemoryIndexEntry[];
  memoryDocs: Record<string, MemoryDoc>;
  agents: DirectoryAgent[];
  humans: DirectoryHuman[];
}

export interface TeamProjectLink {
  team_id: string;
  project_id: string;
  name: string;
  glyph: string;
  repo: string;
  relation: 'primary' | 'linked';
}

function seed(): TeamsData {
  const teams: TeamView[] = [
    {
      id: 'team-7c19b0',
      org_id: 'org-ooo',
      name: 'agent-center core',
      glyph: 'AC',
      status: 'active',
      description: '主库特性开发编队 —— 规划、实现、评审闭环。',
      projects_count: 2,
      members_count: 6,
      created: '2026/6/12',
      version: 3,
      roles: [
        { role: 'planner', count: 1, cli: 'claude-code', model: 'opus-4.8', max_concurrency: 1, capability_tags: [] },
        { role: 'coder', count: 3, cli: 'claude-code', model: 'sonnet-5', max_concurrency: 2, capability_tags: [] },
        { role: 'reviewer', count: 1, cli: 'claude-code', model: 'opus-4.8', max_concurrency: 1, capability_tags: [] },
        { role: 'ops', count: 1, cli: 'codex', model: 'gpt-5', max_concurrency: 1, capability_tags: [] },
      ],
    },
    {
      id: 'team-4a1f22',
      org_id: 'org-ooo',
      name: 'growth-experiments',
      glyph: 'GX',
      status: 'active',
      description: '增长实验小队，快速原型 + 数据回收。',
      projects_count: 1,
      members_count: 4,
      created: '2026/6/28',
      version: 1,
      roles: [
        { role: 'planner', count: 1, cli: 'claude-code', model: 'sonnet-5', max_concurrency: 1, capability_tags: [] },
        { role: 'coder', count: 2, cli: 'claude-code', model: 'sonnet-5', max_concurrency: 2, capability_tags: [] },
        { role: 'researcher', count: 1, cli: 'claude-code', model: 'opus-4.8', max_concurrency: 1, capability_tags: [] },
      ],
    },
    {
      id: 'team-9b8e01',
      org_id: 'org-ooo',
      name: 'docs-and-dx',
      glyph: 'DX',
      status: 'draft',
      description: '文档与开发者体验（草稿，未实例化）。',
      projects_count: 0,
      members_count: 0,
      created: '2026/7/10',
      version: 1,
      roles: [
        { role: 'designer', count: 1, cli: 'claude-code', model: 'opus-4.8', max_concurrency: 1, capability_tags: [] },
        { role: 'coder', count: 1, cli: 'claude-code', model: 'sonnet-5', max_concurrency: 1, capability_tags: [] },
      ],
    },
  ];

  const members: Record<string, MemberView[]> = {
    'team-7c19b0': [
      { team_id: 'team-7c19b0', member_ref: 'agent:9a70…', name: 'planner-01', kind: 'agent', role: 'planner', exclusive: true, cli: 'claude-code', model: 'opus-4.8', tags: ['design', 'arch'], concurrency: '1/1' },
      { team_id: 'team-7c19b0', member_ref: 'agent:3b21…', name: 'coder-01', kind: 'agent', role: 'coder', exclusive: true, cli: 'claude-code', model: 'sonnet-5', tags: ['go'], concurrency: '2/2' },
      { team_id: 'team-7c19b0', member_ref: 'agent:7f44…', name: 'coder-02', kind: 'agent', role: 'coder', exclusive: true, cli: 'claude-code', model: 'sonnet-5', tags: ['ts', 'react'], concurrency: '1/2' },
      { team_id: 'team-7c19b0', member_ref: 'agent:c012…', name: 'coder-03', kind: 'agent', role: 'coder', exclusive: true, cli: 'claude-code', model: 'sonnet-5', tags: ['go'], concurrency: '2/2' },
      { team_id: 'team-7c19b0', member_ref: 'agent:e5a9…', name: 'reviewer-01', kind: 'agent', role: 'reviewer', exclusive: true, cli: 'claude-code', model: 'opus-4.8', tags: ['security'], concurrency: '1/1' },
      { team_id: 'team-7c19b0', member_ref: 'user:owner', name: 'oopslink', kind: 'human', role: 'ops', exclusive: false, cli: '—', model: '—', tags: ['owner'], concurrency: '—' },
    ],
    'team-4a1f22': [],
    'team-9b8e01': [],
  };

  const projects: Record<string, TeamProjectLink[]> = {
    'team-7c19b0': [
      { team_id: 'team-7c19b0', project_id: 'project-c7073e48', name: 'agent-center2', glyph: 'AC', repo: 'primary repo: agent-center', relation: 'primary' },
      { team_id: 'team-7c19b0', project_id: 'project-11f0aa', name: 'docs-site', glyph: 'DX', repo: 'repo: docs', relation: 'linked' },
    ],
    'team-4a1f22': [],
    'team-9b8e01': [],
  };

  const memoryIndex: MemoryIndexEntry[] = [
    { slug: 'MEMORY.md', pinned: true },
    { group: 'entries/' },
    { slug: 'entries/ci-runbook' },
    { slug: 'entries/go-error-patterns' },
    { slug: 'entries/release-checklist' },
    { group: 'rules/' },
    { slug: 'rules/review-gate' },
    { slug: 'rules/scope-discipline' },
  ];

  const memoryDocs: Record<string, MemoryDoc> = {
    'MEMORY.md': {
      slug: 'MEMORY.md',
      path: 'team-memory/MEMORY.md',
      title: 'MEMORY.md · 团队索引',
      frontmatter: null,
      body:
        '团队常驻记忆索引。**此文件常驻加载**进每个成员上下文；`entries/<slug>.md` 条目按需拉取。\n\n' +
        '#### Entries\n\n' +
        '- **entries/ci-runbook** — CI/CD 部署与回滚\n' +
        '- **entries/go-error-patterns** — 错误处理约定\n' +
        '- **entries/release-checklist** — 发布清单\n\n' +
        '#### Rules\n\n' +
        '- **rules/review-gate** — 评审阻塞规约\n' +
        '- **rules/scope-discipline** — team/project/agent memory 归属规约\n',
    },
    'entries/ci-runbook': {
      slug: 'entries/ci-runbook',
      path: 'team-memory/entries/ci-runbook.md',
      title: 'CI/CD runbook',
      frontmatter: 'name: ci-runbook\ntype: reference\nupdated: 2026-07-11',
      body: '#### 触发\n\npush 到 `main` 触发 `ci.yml`；tag `v*` 触发 release。\n\n#### 回滚\n\n- revert 合并提交，让流水线重跑\n- 或 dashboard 手动 promote 上一个 green build\n',
    },
    'entries/go-error-patterns': {
      slug: 'entries/go-error-patterns',
      path: 'team-memory/entries/go-error-patterns.md',
      title: 'Go 错误处理约定',
      frontmatter: 'name: go-error-patterns\ntype: reference\nupdated: 2026-07-05',
      body: '包装用 `fmt.Errorf("…: %w", err)`；哨兵错误集中在 `errors.go`。禁止吞错。\n',
    },
    'entries/release-checklist': {
      slug: 'entries/release-checklist',
      path: 'team-memory/entries/release-checklist.md',
      title: '发布清单',
      frontmatter: 'name: release-checklist\ntype: project\nupdated: 2026-07-12',
      body: '- migration 已跑 + 可回滚\n- coverage ≥ 基线\n- CHANGELOG 更新\n- owner 签发\n',
    },
    'rules/review-gate': {
      slug: 'rules/review-gate',
      path: 'team-memory/rules/review-gate.md',
      title: 'Review gate rule',
      frontmatter: 'name: review-gate\ntype: rule\nupdated: 2026-07-13',
      body: '阻塞规约：正确性缺陷、安全问题、无测试的行为改动必须退回；命名和风格问题标记为 nit，不阻塞发布。\n',
    },
    'rules/scope-discipline': {
      slug: 'rules/scope-discipline',
      path: 'team-memory/rules/scope-discipline.md',
      title: 'Memory scope discipline',
      frontmatter: 'name: scope-discipline\ntype: rule\nupdated: 2026-07-13',
      body: '通用团队经验写入 `team-memory/entries/` 或 `team-memory/rules/`；项目事实写 project memory；个人偏好和局部操作痕迹写 agent memory。\n',
    },
  };

  const agents: DirectoryAgent[] = [
    { ref: 'agent:agent-pd', name: 'agent-center-pd', status: 'working', role: 'planner', teams: ['agent-center core'], team_ids: ['team-7c19b0'], model: 'opus-4.8', load: 0.4, backlog: 1, last: 'now' },
    { ref: 'agent:agent-d1', name: 'agent-center-dev1', status: 'idle', role: 'coder', teams: ['agent-center core'], team_ids: ['team-7c19b0'], model: 'sonnet-5', load: 0.0, backlog: 0, last: '2m' },
    { ref: 'agent:agent-d2', name: 'agent-center-dev2', status: 'idle', role: 'coder', teams: ['agent-center core'], team_ids: ['team-7c19b0'], model: 'sonnet-5', load: 0.0, backlog: 0, last: '5m' },
    { ref: 'agent:agent-d3', name: 'agent-center-dev3', status: 'idle', role: 'coder', teams: ['agent-center core'], team_ids: ['team-7c19b0'], model: 'sonnet-5', load: 0.0, backlog: 0, last: '8m' },
    { ref: 'agent:agent-t1', name: 'agent-center-tester1', status: 'idle', role: 'reviewer', teams: ['agent-center core'], team_ids: ['team-7c19b0'], model: 'opus-4.8', load: 0.0, backlog: 0, last: '12m' },
    { ref: 'agent:agent-t2', name: 'agent-center-tester2', status: 'idle', role: 'reviewer', teams: ['growth-experiments'], team_ids: ['team-4a1f22'], model: 'sonnet-5', load: 0.0, backlog: 0, last: '20m' },
    { ref: 'agent:agent-t3', name: 'agent-center-tester3', status: 'idle', role: 'researcher', teams: ['growth-experiments'], team_ids: ['team-4a1f22'], model: 'opus-4.8', load: 0.0, backlog: 0, last: '25m' },
    { ref: 'agent:agent-d4', name: 'agent-center-dev4', status: 'idle', role: 'coder', teams: ['growth-experiments'], team_ids: ['team-4a1f22'], model: 'sonnet-5', load: 0.0, backlog: 0, last: '31m' },
    { ref: 'agent:agent-d5', name: 'agent-center-dev5', status: 'idle', role: 'coder', teams: [], team_ids: [], model: 'sonnet-5', load: 0.0, backlog: 0, last: '1h' },
    { ref: 'agent:agent-intd', name: 'agent-center-integration-d', status: 'idle', role: 'ops', teams: [], team_ids: [], model: 'gpt-5', load: 0.0, backlog: 0, last: '2h' },
    { ref: 'agent:agent-ude', name: 'UDE', status: 'working', role: 'designer', teams: ['docs-and-dx'], team_ids: ['team-9b8e01'], model: 'opus-4.8', load: 0.6, backlog: 2, last: 'now' },
  ];

  const humans: DirectoryHuman[] = [
    { ref: 'user:user-oops', name: 'oopslink', role: 'owner', status: 'Joined', email: 'oopslink@abc.com', created: '2026/6/4', last: '2026/7/13', teams: ['agent-center core', 'growth-experiments', 'docs-and-dx'], team_ids: ['team-7c19b0', 'team-4a1f22', 'team-9b8e01'] },
    { ref: 'user:user-alice', name: 'alice', role: 'member', status: 'Joined', email: 'alice@abc.com', created: '2026/6/18', last: '1h', teams: ['agent-center core', 'growth-experiments'], team_ids: ['team-7c19b0', 'team-4a1f22'] },
    { ref: 'user:user-bob', name: 'bob', role: 'member', status: 'Joined', email: 'bob@abc.com', created: '2026/6/25', last: '3h', teams: ['docs-and-dx'], team_ids: ['team-9b8e01'] },
    { ref: 'user:user-carol', name: 'carol', role: 'member', status: 'Invited', email: 'carol@abc.com', created: '2026/7/9', last: '—', teams: [], team_ids: [] },
  ];

  return {
    teams,
    members,
    projects,
    memoryIndex,
    memoryDocs,
    agents,
    humans,
  };
}

// A structured deep clone so mutations never leak back into the seed.
function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T;
}

let store: TeamsData = seed();

export function teamsStore(): TeamsData {
  return store;
}

// Test hook: reset to the pristine seed between cases.
export function resetTeamsStore(): void {
  store = seed();
}

export { clone as cloneTeamsValue };
