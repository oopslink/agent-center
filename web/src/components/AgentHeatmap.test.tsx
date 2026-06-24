import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import type { HeatmapCell } from '@/api/types';
import { AgentHeatmap } from './AgentHeatmap';

afterEach(() => cleanup());

// Fixed "now" so the 53×7 window is deterministic. 2026-06-23 is a Tuesday (UTC).
const TODAY = new Date('2026-06-23T12:00:00Z');

function cell(day: string, over: Partial<HeatmapCell> = {}): HeatmapCell {
  return { day, events: 0, tokens_in: 0, tokens_out: 0, cache_tokens: 0, cost_micros: 0, ...over };
}

function cellAt(container: HTMLElement, date: string): HTMLElement | null {
  return container.querySelector(`[data-testid="heatmap-cell"][data-date="${date}"]`);
}

describe('AgentHeatmap', () => {
  it('renders the card, the 3-way 口径 switch, and a 5-step legend', () => {
    render(<AgentHeatmap cells={[]} today={TODAY} />);
    expect(screen.getByTestId('agent-heatmap')).toBeInTheDocument();
    expect(screen.getByText(/Activity Heatmap · last 12 months/)).toBeInTheDocument();

    const sw = screen.getByTestId('heatmap-metric-switch');
    expect(within(sw).getAllByRole('tab')).toHaveLength(3);
    expect(screen.getByTestId('heatmap-metric-activity')).toHaveAttribute('aria-selected', 'true');

    // legend: Less + 5 swatches (level 0..4) + More
    for (const lvl of [0, 1, 2, 3, 4]) {
      expect(screen.getByTestId(`heatmap-legend-${lvl}`)).toBeInTheDocument();
    }
    expect(screen.getByText('Less')).toBeInTheDocument();
    expect(screen.getByText('More')).toBeInTheDocument();
  });

  it('lays out 53 week-columns and never renders a future day as a cell', () => {
    const { container } = render(<AgentHeatmap cells={[]} today={TODAY} />);
    expect(container.querySelectorAll('[role="row"]')).toHaveLength(53);

    const dates = Array.from(container.querySelectorAll('[data-testid="heatmap-cell"]')).map(
      (el) => el.getAttribute('data-date') ?? '',
    );
    expect(dates.length).toBeGreaterThan(0);
    // today is included; nothing after it is a real cell.
    expect(dates).toContain('2026-06-23');
    expect(dates.every((d) => d <= '2026-06-23')).toBe(true);
  });

  it('buckets intensity 1..4 relative to the window max for the active 口径', () => {
    const cells = [
      cell('2026-06-20', { events: 100 }), // max → level 4
      cell('2026-06-21', { events: 60 }), //  0.6 → level 3
      cell('2026-06-22', { events: 30 }), //  0.3 → level 2
      cell('2026-06-23', { events: 10 }), //  0.1 → level 1
    ];
    const { container } = render(<AgentHeatmap cells={cells} today={TODAY} />);
    expect(cellAt(container, '2026-06-20')).toHaveAttribute('data-level', '4');
    expect(cellAt(container, '2026-06-21')).toHaveAttribute('data-level', '3');
    expect(cellAt(container, '2026-06-22')).toHaveAttribute('data-level', '2');
    expect(cellAt(container, '2026-06-23')).toHaveAttribute('data-level', '1');
  });

  it('an empty day reads level 0 with a zero-valued tooltip', () => {
    const { container } = render(<AgentHeatmap cells={[]} today={TODAY} />);
    const c = cellAt(container, '2026-06-23');
    expect(c).toHaveAttribute('data-level', '0');
    expect(c).toHaveAttribute('title', 'Jun 23, 2026: 0 events');
    expect(c).toHaveAttribute('aria-label', 'Jun 23, 2026: 0 events');
  });

  it('switching 口径 re-colors off a different field and updates the tooltip', () => {
    // A day that is low on events but high on tokens: its bucket must move when
    // the 口径 switches from Activity to Tokens (color semantics unchanged).
    const cells = [
      cell('2026-06-20', { events: 100, tokens_in: 1, tokens_out: 0, cost_micros: 0 }), // events max
      cell('2026-06-23', { events: 1, tokens_in: 900, tokens_out: 100, cost_micros: 2_500_000 }), // tokens max
    ];
    const { container } = render(<AgentHeatmap cells={cells} today={TODAY} />);

    // Activity 口径: 2026-06-23 is tiny (1/100) → level 1.
    const day = cellAt(container, '2026-06-23');
    expect(day).toHaveAttribute('data-level', '1');
    expect(day).toHaveAttribute('title', 'Jun 23, 2026: 1 event');

    // Switch to Tokens: 2026-06-23 (1000 tokens) is now the window max → level 4.
    fireEvent.click(screen.getByTestId('heatmap-metric-tokens'));
    expect(screen.getByTestId('heatmap-metric-tokens')).toHaveAttribute('aria-selected', 'true');
    const dayTokens = cellAt(container, '2026-06-23');
    expect(dayTokens).toHaveAttribute('data-level', '4');
    expect(dayTokens).toHaveAttribute('title', 'Jun 23, 2026: 1,000 tokens');

    // Switch to Cost: tooltip shows USD from cost_micros (2_500_000 → $2.50).
    fireEvent.click(screen.getByTestId('heatmap-metric-cost'));
    expect(cellAt(container, '2026-06-23')).toHaveAttribute('title', 'Jun 23, 2026: $2.50');
  });

  it('exposes the active 口径 in the grid aria-label', () => {
    render(<AgentHeatmap cells={[]} today={TODAY} initialMetric="cost" />);
    expect(screen.getByRole('grid')).toHaveAttribute('aria-label', expect.stringContaining('Cost'));
  });

  it('each 口径 owns a distinct hue ramp (green / blue / amber) via inline style — T474', () => {
    render(<AgentHeatmap cells={[]} today={TODAY} />);
    // T474: levels 1-4 use a data-viz inline-style hue ramp (alpha-on-token was
    // transparent). The level-4 swatch's backgroundColor is the metric's solid hue
    // and differs per 口径; level-3 differs from level-4 (a real ramp, not blank).
    const green4 = screen.getByTestId('heatmap-legend-4').style.backgroundColor;
    const green3 = screen.getByTestId('heatmap-legend-3').style.backgroundColor;
    expect(green4).not.toBe('');
    expect(green3).not.toBe('');
    expect(green3).not.toBe(green4);

    fireEvent.click(screen.getByTestId('heatmap-metric-tokens'));
    const blue4 = screen.getByTestId('heatmap-legend-4').style.backgroundColor;
    expect(blue4).not.toBe('');
    expect(blue4).not.toBe(green4);

    fireEvent.click(screen.getByTestId('heatmap-metric-cost'));
    const amber4 = screen.getByTestId('heatmap-legend-4').style.backgroundColor;
    expect(amber4).not.toBe('');
    expect(amber4).not.toBe(blue4);
    // level 0 stays the neutral token (no inline hue).
    expect(screen.getByTestId('heatmap-legend-0').className).toContain('bg-bg-subtle');
  });
});
