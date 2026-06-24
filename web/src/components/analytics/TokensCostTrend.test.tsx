import { afterEach, describe, expect, it } from 'vitest';
import { render, cleanup, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { TokensCostTrend } from './TokensCostTrend';
import type { AnalyticsModelTrendPoint, AnalyticsProjectTrendPoint } from '@/api/types';

const byModel: AnalyticsModelTrendPoint[] = [
  { day: '2026-05-01', model: 'claude-opus-4-8', tokens_in: 100, tokens_out: 50, cache_tokens: 0, cost_micros: 1000 },
  { day: '2026-06-01', model: 'claude-opus-4-8', tokens_in: 200, tokens_out: 80, cache_tokens: 0, cost_micros: 2000 },
  { day: '2026-06-01', model: 'claude-sonnet-4-6', tokens_in: 20, tokens_out: 10, cache_tokens: 0, cost_micros: 100 },
];
const byProject: AnalyticsProjectTrendPoint[] = [
  { day: '2026-05-01', project_id: 'p1', events: 1, tokens_in: 50, tokens_out: 25, cache_tokens: 0, cost_micros: 500 },
  { day: '2026-06-01', project_id: '', events: 1, tokens_in: 10, tokens_out: 5, cache_tokens: 0, cost_micros: 50 },
];

// T472: the project legend resolves project_id → name + links to project detail, so
// the component now reads useProjects (QueryClient) + renders OrgLink (Router).
function renderTrend(props: Parameters<typeof TokensCostTrend>[0]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <TokensCostTrend {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TokensCostTrend', () => {
  afterEach(() => cleanup());

  it('defaults to Tokens × Model with a stacked area + legend per model', () => {
    renderTrend({ byModel, byProject });
    expect(screen.getByTestId('analytics-trend-svg')).toBeInTheDocument();
    const legend = screen.getByTestId('analytics-trend-legend');
    expect(within(legend).getByText('opus')).toBeInTheDocument();
    expect(within(legend).getByText('sonnet')).toBeInTheDocument();
  });

  it('switches to the project dimension, relabelling the legend ("" → "(no project)")', () => {
    renderTrend({ byModel, byProject });
    fireEvent.click(within(screen.getByTestId('trend-dim-toggle')).getByText('Project'));
    const legend = screen.getByTestId('analytics-trend-legend');
    // no project-name data mocked → falls back to the raw id.
    expect(within(legend).getByText('p1')).toBeInTheDocument();
    expect(within(legend).getByText('(no project)')).toBeInTheDocument();
  });

  // T472: resolve the project name + link to /projects/:id.
  it('shows the project NAME (resolved) and links it to project detail', async () => {
    server.use(
      http.get('/api/projects', () =>
        HttpResponse.json({ projects: [{ id: 'p1', name: 'Acme Revamp', organization_id: 'o1' }] }),
      ),
    );
    renderTrend({ byModel, byProject });
    fireEvent.click(within(screen.getByTestId('trend-dim-toggle')).getByText('Project'));
    const link = await screen.findByTestId('trend-legend-project-link');
    await waitFor(() => expect(link).toHaveTextContent('Acme Revamp'));
    expect(link).toHaveAttribute('href', expect.stringContaining('/projects/p1'));
    // the "(no project)" bucket stays plain text (no link).
    expect(within(screen.getByTestId('analytics-trend-legend')).getByText('(no project)')).toBeInTheDocument();
  });

  it('shows an empty state when the selected metric has no data', () => {
    renderTrend({ byModel: [], byProject: [] });
    expect(screen.getByTestId('analytics-trend-empty')).toBeInTheDocument();
  });
});
