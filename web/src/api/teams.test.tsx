import { beforeEach, describe, expect, it } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { makeWrapper } from '@/test/renderWith';
import {
  exportTemplateEnvelope,
  roleColor,
  useCreateTeam,
  useDeleteTeam,
  useSaveTemplate,
  useTeamMemoryDoc,
  useTeams,
  type TeamTemplate,
} from './teams';
import { resetTeamsStore, teamsStore } from './teamsFixtures';

describe('teams api (fixture-backed)', () => {
  beforeEach(() => resetTeamsStore());

  it('roleColor falls back for unknown roles', () => {
    expect(roleColor('planner')).toBe('#7C3AED');
    expect(roleColor('mystery')).toBe('#8b8794');
  });

  it('exportTemplateEnvelope emits a team-template/v1 doc', () => {
    const t = teamsStore().templates[0] as TeamTemplate;
    const env = exportTemplateEnvelope(t) as Record<string, unknown>;
    expect(env.format).toBe('team-template/v1');
    expect(env.source_id).toBe(t.id);
    expect(Array.isArray(env.roles)).toBe(true);
  });

  it('useTeams resolves the seeded list', async () => {
    const { result } = renderHook(() => useTeams(), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(3);
  });

  it('useCreateTeam then useDeleteTeam mutate the store', async () => {
    const wrapper = makeWrapper();
    const create = renderHook(() => useCreateTeam(), { wrapper });
    await act(async () => {
      await create.result.current.mutateAsync({
        name: 'x-team',
        description: '',
        visibility: 'org-private',
        roles: [{
          role: 'coder',
          cli: 'claude-code',
          model: 'sonnet-5',
          max_concurrency: 1,
          count: 1,
          tags: 'go, ts',
          ram_role_keys: ['Team contributor'],
          access_requirements: ['project.read'],
        }],
      });
    });
    expect(teamsStore().teams.some((t) => t.name === 'x-team')).toBe(true);
    const created = teamsStore().teams.find((t) => t.name === 'x-team')!;
    expect(created.roles[0].capability_tags).toEqual(['go', 'ts']);
    expect(created.roles[0].ram_role_keys).toEqual(['Team contributor']);
    expect(created.roles[0].access_requirements).toEqual(['project.read']);

    const del = renderHook(() => useDeleteTeam(), { wrapper });
    await act(async () => {
      await del.result.current.mutateAsync(created.id);
    });
    expect(teamsStore().teams.some((t) => t.id === created.id)).toBe(false);
  });

  it('useSaveTemplate persists source metadata and role ram_role_keys', async () => {
    const wrapper = makeWrapper();
    const save = renderHook(() => useSaveTemplate(), { wrapper });
    await act(async () => {
      await save.result.current.mutateAsync({
        name: 'curated delivery team',
        description: 'source-backed template',
        source: 'extract:team-7c19b0',
        source_kind: 'extract',
        roles: [{
          role: 'reviewer',
          cli: 'claude-code',
          model: 'sonnet-5',
          max_concurrency: 1,
          count: 1,
          capability_tags: [],
          ram_role_keys: ['Team curator'],
          access_requirements: ['team.read', 'team.memory.review'],
        }],
      });
    });
    const saved = teamsStore().templates.find((t) => t.name === 'curated delivery team')!;
    expect(saved.source).toBe('extract:team-7c19b0');
    expect(saved.source_kind).toBe('extract');
    expect(saved.roles[0].ram_role_keys).toEqual(['Team curator']);
  });

  it('useTeamMemoryDoc throws for an unknown slug', async () => {
    const { result } = renderHook(() => useTeamMemoryDoc('team-7c19b0', 'nope'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
