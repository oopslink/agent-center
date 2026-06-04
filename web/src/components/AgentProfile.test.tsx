import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { AgentProfile } from './AgentProfile';
import type { Agent } from '@/api/types';

const base: Agent = {
  id: 'A1',
  organization_id: 'O-1',
  name: 'bot-1',
  description: 'a helper',
  model: 'claude-opus-4-8',
  cli: 'claude-code',
  env_vars: {},
  skills: ['review', 'planning'],
  worker_id: 'w-1',
  lifecycle: 'stopped',
  availability: 'available',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
};

function wrap(agent: Agent) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AgentProfile agent={agent} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AgentProfile (#228 PR(b))', () => {
  afterEach(() => cleanup());

  it('shows the bound computer name + connected status (#120)', () => {
    wrap({
      ...base,
      computer: { worker_id: 'w-1', name: 'box-7', status: 'online', connected: true },
    });
    expect(screen.getByTestId('agent-profile-computer-name')).toHaveTextContent('box-7');
    // raw worker id is on hover, not visible text (#192).
    expect(screen.getByTestId('agent-profile-computer-name')).toHaveAttribute('title', 'w-1');
    const status = screen.getByTestId('agent-profile-computer-status');
    expect(status).toHaveTextContent('online');
    expect(status).toHaveAttribute('data-connected', 'true');
  });

  it('falls back to "no worker" when computer is absent', () => {
    wrap(base);
    expect(screen.getByTestId('agent-profile-computer')).toHaveTextContent('no worker');
  });

  it('shows the creator display name (raw ref on hover), not the raw actor ref', () => {
    wrap({ ...base, created_by_display_name: 'Hayang' });
    const creator = screen.getByTestId('agent-profile-creator-ref');
    expect(creator).toHaveTextContent('Hayang');
    expect(creator).toHaveAttribute('title', 'user:hayang');
    expect(screen.getByTestId('agent-profile-creator')).not.toHaveTextContent('user:hayang');
  });

  it('renders runtime config: CLI/Model real + reasoning/mode/provider static defaults', () => {
    wrap(base);
    expect(screen.getByTestId('agent-profile-tag-cli')).toHaveTextContent('claude-code');
    expect(screen.getByTestId('agent-profile-tag-model')).toHaveTextContent('claude-opus-4-8');
    expect(screen.getByTestId('agent-profile-tag-reasoning')).toHaveTextContent('Medium');
    expect(screen.getByTestId('agent-profile-tag-mode')).toHaveTextContent('Default');
    expect(screen.getByTestId('agent-profile-tag-provider')).toHaveTextContent('Default');
    // static fallbacks are labelled "default" (not presented as stored values).
    expect(screen.getByTestId('agent-profile-tag-reasoning')).toHaveTextContent(/default/i);
    expect(screen.getByTestId('agent-profile-tag-cli')).not.toHaveTextContent(/default/i);
  });

  it('renders skills as name cards (no path/badge), empty → placeholder', () => {
    wrap(base);
    expect(screen.getAllByTestId('agent-profile-skill')).toHaveLength(2);
    cleanup();
    wrap({ ...base, skills: [] });
    expect(screen.getByTestId('agent-profile-skills-empty')).toBeInTheDocument();
  });

  it('lists created agents linking to each detail; empty → placeholder', () => {
    wrap({ ...base, created_agents: [{ id: 'sub-1', name: 'helper' }] });
    const link = screen.getByTestId('agent-profile-created-agent').querySelector('a');
    expect(link).toHaveTextContent('helper');
    expect(link?.getAttribute('href')).toContain('/agents/sub-1');
    cleanup();
    wrap(base);
    expect(screen.getByTestId('agent-profile-created-agents-empty')).toBeInTheDocument();
  });

  // v2.7.1 #240: the Message action moved out of the Profile body into the
  // AgentDetail header — covered by AgentDetail.test now.
});
