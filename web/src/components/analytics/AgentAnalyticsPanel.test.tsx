import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/renderWith';
import { server } from '@/test/mswServer';
import { AgentAnalyticsPanel } from './AgentAnalyticsPanel';

function payload() {
  const today = new Date().toISOString().slice(0, 10);
  return {
    agent_id: 'a1',
    agent_ref: 'agent:a1',
    from: today,
    to: today,
    heatmap: [
      { day: today, events: 3, completed: 1, tokens_in: 100, tokens_out: 50, cache_tokens: 0, cost_micros: 1000 },
    ],
    overview: {
      today: { tokens_in: 0, tokens_out: 0, cache_tokens: 0, cost_micros: 0, completed_tasks: 0 },
      week: { tokens_in: 0, tokens_out: 0, cache_tokens: 0, cost_micros: 0, completed_tasks: 0 },
      month: { tokens_in: 0, tokens_out: 0, cache_tokens: 0, cost_micros: 0, completed_tasks: 0 },
      active_days: 1,
      streak: 1,
    },
    trends: {
      by_project: [],
      by_model: [
        { day: today, model: 'claude-opus-4-8', tokens_in: 100, tokens_out: 50, cache_tokens: 0, cost_micros: 1000 },
      ],
    },
    top_tasks: [
      {
        task_id: 'task-1',
        title: 'Build the thing',
        dominant_model: 'claude-opus-4-8',
        events: 3,
        tokens_in: 100,
        tokens_out: 50,
        cache_tokens: 0,
        cost_micros: 1000,
      },
    ],
  };
}

describe('AgentAnalyticsPanel', () => {
  afterEach(() => cleanup());

  it('composes overview cards + trend + top tasks on success', async () => {
    server.use(http.get('/api/agents/:id/analytics', () => HttpResponse.json(payload())));
    renderWithQuery(<AgentAnalyticsPanel agentId="a1" />);
    await waitFor(() => expect(screen.getByTestId('agent-analytics')).toBeInTheDocument());
    expect(screen.getByTestId('analytics-overview-cards')).toBeInTheDocument();
    // F7: the F5 heatmap renders between the cards and the trend, fed by the same
    // series fetch (no extra request).
    expect(screen.getByTestId('agent-heatmap')).toBeInTheDocument();
    expect(screen.getByTestId('analytics-trend')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('analytics-top-tasks')).toBeInTheDocument());
    expect(screen.getByTestId('top-task-label-task-1')).toHaveTextContent('Build the thing');
  });

  it('shows an error surface when the series read fails', async () => {
    server.use(
      http.get('/api/agents/:id/analytics', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
    );
    renderWithQuery(<AgentAnalyticsPanel agentId="a1" />);
    await waitFor(() => expect(screen.getByTestId('agent-analytics-error')).toBeInTheDocument());
  });
});
