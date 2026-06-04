import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { AgentActivityRow } from './AgentActivityRow';
import type { AgentActivityEvent } from '@/api/types';

function ev(event_type: string, payload: unknown, extra: Partial<AgentActivityEvent> = {}): AgentActivityEvent {
  return {
    id: 'AC-1',
    agent_id: 'A1',
    event_type,
    payload: typeof payload === 'string' ? payload : JSON.stringify(payload),
    occurred_at: '2026-06-04T01:00:00Z',
    ...extra,
  };
}

function row(e: AgentActivityEvent) {
  return render(
    <table>
      <tbody>
        <AgentActivityRow event={e} />
      </tbody>
    </table>,
  );
}

describe('AgentActivityRow (#216)', () => {
  afterEach(() => cleanup());

  it('assistant_text → Assistant badge + truncated preview', () => {
    const long = 'x'.repeat(200);
    row(ev('assistant_text', { text: long }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Assistant');
    const preview = screen.getByTestId('agent-activity-preview').textContent ?? '';
    expect(preview.endsWith('…')).toBe(true);
    expect(preview.length).toBeLessThan(130);
  });

  it('tool_use → Tool badge + ToolName(args) summary', () => {
    row(ev('tool_use', { tool_name: 'read_file', args: { path: '/x' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Tool');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('read_file(');
  });

  it('tool_result with ok=false renders a danger badge + duration/tokens', () => {
    row(ev('tool_result', { tool_name: 'run', duration_ms: 120, tokens: 50, ok: false }));
    expect(screen.getByTestId('agent-activity-badge').className).toContain('text-danger');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('120ms');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('50 tok');
  });

  it('system_init → model · session prefix', () => {
    row(ev('system_init', { model: 'claude-opus-4-8', session_id: 'abcd1234efgh' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('System');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('claude-opus-4-8');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('abcd1234');
  });

  it('status_change → from → to', () => {
    row(ev('status_change', { from: 'queued', to: 'active' }));
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('queued → active');
  });

  it('thinking → Thinking badge + truncated text preview', () => {
    row(ev('thinking', { text: 'pondering '.repeat(30) }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Thinking');
    const preview = screen.getByTestId('agent-activity-preview').textContent ?? '';
    expect(preview.endsWith('…')).toBe(true);
  });

  it('result → Turn badge with total tokens + rounded cost; danger color on error', () => {
    // Raw float cost must be rounded in the summary (not $0.27182375…).
    row(ev('result', { tokens_in: 100, tokens_out: 40, cost_usd: 0.27182375000000003, is_error: false }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Turn');
    expect(screen.getByTestId('agent-activity-badge').className).toContain('text-success');
    const preview = screen.getByTestId('agent-activity-preview');
    expect(preview).toHaveTextContent('140 tok');
    expect(preview).toHaveTextContent('$0.2718');
    expect(preview.textContent).not.toContain('0.27182375');
    cleanup();
    row(ev('result', { tokens_in: 10, tokens_out: 0, is_error: true }));
    expect(screen.getByTestId('agent-activity-badge').className).toContain('text-danger');
  });

  it('rate_limit → Rate limit (danger) badge + message preview', () => {
    row(ev('rate_limit', { message: 'slow down' }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Rate limit');
    expect(badge.className).toContain('text-danger');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('slow down');
  });

  it('unknown event type → falls back to its type as the badge + JSON preview', () => {
    row(ev('weird.event', { foo: 'bar' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('weird.event');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('foo');
  });

  it('toggles an expanded JSON payload (debug view) + shows refs', () => {
    row(ev('tool_use', { tool_name: 'read_file' }, { work_item_ref: 'agent://WI-1', interaction_ref: 'int-9' }));
    expect(screen.queryByTestId('agent-activity-payload-json')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    expect(screen.getByTestId('agent-activity-payload-json')).toHaveTextContent('read_file');
    expect(screen.getByTestId('agent-activity-workitem-ref')).toHaveTextContent('agent://WI-1');
    expect(screen.getByTestId('agent-activity-interaction-ref')).toHaveTextContent('int-9');
    // collapse again
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    expect(screen.queryByTestId('agent-activity-payload-json')).not.toBeInTheDocument();
  });

  it('survives malformed JSON payload without crashing', () => {
    row(ev('assistant_text', 'not json {'));
    expect(screen.getByTestId('agent-activity-row')).toBeInTheDocument();
  });
});
