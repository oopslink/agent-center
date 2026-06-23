import { afterEach, describe, expect, it } from 'vitest';
import { render, cleanup, screen, fireEvent, within } from '@testing-library/react';
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

describe('TokensCostTrend', () => {
  afterEach(() => cleanup());

  it('defaults to Tokens × Model with a stacked area + legend per model', () => {
    render(<TokensCostTrend byModel={byModel} byProject={byProject} />);
    expect(screen.getByTestId('analytics-trend-svg')).toBeInTheDocument();
    const legend = screen.getByTestId('analytics-trend-legend');
    expect(within(legend).getByText('opus')).toBeInTheDocument();
    expect(within(legend).getByText('sonnet')).toBeInTheDocument();
  });

  it('switches to the project dimension, relabelling the legend ("" → "(no project)")', () => {
    render(<TokensCostTrend byModel={byModel} byProject={byProject} />);
    fireEvent.click(within(screen.getByTestId('trend-dim-toggle')).getByText('Project'));
    const legend = screen.getByTestId('analytics-trend-legend');
    expect(within(legend).getByText('p1')).toBeInTheDocument();
    expect(within(legend).getByText('(no project)')).toBeInTheDocument();
  });

  it('shows an empty state when the selected metric has no data', () => {
    render(<TokensCostTrend byModel={[]} byProject={[]} />);
    expect(screen.getByTestId('analytics-trend-empty')).toBeInTheDocument();
  });
});
