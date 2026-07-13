import { beforeEach, describe, expect, it } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { makeWrapper } from '@/test/renderWith';
import {
  exportTemplateEnvelope,
  roleColor,
  useCreateTeam,
  useDeleteTeam,
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
        roles: [{ role: 'coder', cli: 'claude-code', model: 'sonnet-5', max_concurrency: 1, count: 1, tags: 'go, ts' }],
      });
    });
    expect(teamsStore().teams.some((t) => t.name === 'x-team')).toBe(true);
    const created = teamsStore().teams.find((t) => t.name === 'x-team')!;
    expect(created.roles[0].capability_tags).toEqual(['go', 'ts']);

    const del = renderHook(() => useDeleteTeam(), { wrapper });
    await act(async () => {
      await del.result.current.mutateAsync(created.id);
    });
    expect(teamsStore().teams.some((t) => t.id === created.id)).toBe(false);
  });

  it('useTeamMemoryDoc throws for an unknown slug', async () => {
    const { result } = renderHook(() => useTeamMemoryDoc('team-7c19b0', 'nope'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
