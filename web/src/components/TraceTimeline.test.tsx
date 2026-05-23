import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent } from '@testing-library/react';
import { TraceTimeline } from './TraceTimeline';
import type { TraceEvent } from '@/api/types';

const ev = (id: string, type: string, payload?: Record<string, unknown>): TraceEvent => ({
  id,
  event_type: type,
  occurred_at: '2026-05-24T01:00:00Z',
  payload,
});

describe('TraceTimeline', () => {
  afterEach(() => cleanup());

  it('renders empty placeholder when no events', () => {
    render(<TraceTimeline events={[]} />);
    expect(screen.getByTestId('trace-empty')).toBeInTheDocument();
  });

  it('renders one row per event', () => {
    render(<TraceTimeline events={[ev('1', 'tool.call'), ev('2', 'tool.result')]} />);
    expect(screen.getAllByTestId('trace-row')).toHaveLength(2);
  });

  it('rows without payload are not expandable', () => {
    render(<TraceTimeline events={[ev('1', 'tool.call')]} />);
    expect(screen.getByTestId('trace-toggle')).toBeDisabled();
  });

  it('rows with payload toggle JSON visibility', () => {
    render(<TraceTimeline events={[ev('1', 'tool.call', { foo: 'bar' })]} />);
    const toggle = screen.getByTestId('trace-toggle');
    expect(screen.queryByTestId('trace-payload')).not.toBeInTheDocument();
    fireEvent.click(toggle);
    const pre = screen.getByTestId('trace-payload');
    expect(pre).toHaveTextContent(/"foo": "bar"/);
    fireEvent.click(toggle);
    expect(screen.queryByTestId('trace-payload')).not.toBeInTheDocument();
  });
});
