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

// v2.7.1 #228 PR(c): the main badge shows one of 5 user-facing CATEGORIES
// (Output / Thinking / Running command / Searching code / Checking messages);
// the raw event_type lives on data-event-type + in the expanded JSON viewer.
describe('AgentActivityRow (#228 categories)', () => {
  afterEach(() => cleanup());

  it('assistant_text → Output (green) badge + truncated preview', () => {
    const long = 'x'.repeat(200);
    row(ev('assistant_text', { text: long }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Output');
    expect(badge.className).toContain('text-success');
    const preview = screen.getByTestId('agent-activity-preview').textContent ?? '';
    expect(preview.endsWith('…')).toBe(true);
    expect(preview.length).toBeLessThan(130);
  });

  it('result → Output badge with total tokens + rounded cost', () => {
    // Raw float cost must be rounded in the summary (not $0.27182375…).
    row(ev('result', { tokens_in: 100, tokens_out: 40, cost_usd: 0.27182375000000003, is_error: false }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Output');
    const preview = screen.getByTestId('agent-activity-preview');
    expect(preview).toHaveTextContent('140 tok');
    expect(preview).toHaveTextContent('$0.2718');
    expect(preview.textContent).not.toContain('0.27182375');
  });

  it('thinking → Thinking (gray italic) badge + truncated text preview', () => {
    row(ev('thinking', { text: 'pondering '.repeat(30) }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Thinking');
    expect(badge.className).toContain('italic');
    const preview = screen.getByTestId('agent-activity-preview').textContent ?? '';
    expect(preview.endsWith('…')).toBe(true);
  });

  it('tool_use with a non-search tool → Running command badge + ToolName(args)', () => {
    row(ev('tool_use', { tool_name: 'run_shell', args: { cmd: 'ls' } }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Running command');
    expect(badge.className).toContain('text-brand');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('run_shell(');
  });

  it('tool_use with a search tool (Grep) → Searching code badge', () => {
    // Allowlist matches case-insensitively (Grep / read / GlobSearch …).
    row(ev('tool_use', { tool_name: 'Grep', args: { pattern: 'foo' } }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Searching code');
    expect(badge.className).toContain('text-purple-600');
  });

  it('tool_use Read → Searching code (allowlist is case-insensitive)', () => {
    row(ev('tool_use', { tool_name: 'Read', args: { path: '/x' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching code');
  });

  it('tool_use WebSearch / WebFetch → Searching code', () => {
    row(ev('tool_use', { tool_name: 'WebSearch', args: { query: 'foo' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching code');
    cleanup();
    row(ev('tool_use', { tool_name: 'WebFetch', args: { url: 'http://x' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching code');
  });

  it('Bash shell search (rg/find) is NOT Searching code — tool_name=Bash → Running command', () => {
    // Shell searches carry tool_name="Bash"; the command is in tool_input, not
    // tool_name, so they degrade to Running command (PD-accepted, v2.8 deeper).
    row(ev('tool_use', { tool_name: 'Bash', args: { command: 'rg foo' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Running command');
  });

  it('failed tool_result → Running command badge + a "failed" marker + preview', () => {
    row(ev('tool_result', { tool_name: 'run', duration_ms: 120, tokens: 50, ok: false }));
    // Category badge is unchanged; the error shows as a separate red marker.
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Running command');
    const failed = screen.getByTestId('agent-activity-failed');
    expect(failed).toHaveTextContent('failed');
    expect(failed.className).toContain('text-danger');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('120ms');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('50 tok');
  });

  it('successful tool_result → no failed marker', () => {
    row(ev('tool_result', { tool_name: 'run', duration_ms: 5, ok: true }));
    expect(screen.queryByTestId('agent-activity-failed')).not.toBeInTheDocument();
  });

  it('errored result → Output badge + failed marker', () => {
    row(ev('result', { tokens_in: 10, tokens_out: 0, is_error: true }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Output');
    expect(screen.getByTestId('agent-activity-failed')).toBeInTheDocument();
  });

  it('system_init → Checking messages badge + model · session prefix', () => {
    row(ev('system_init', { model: 'claude-opus-4-8', session_id: 'abcd1234efgh' }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Checking messages');
    expect(badge.className).toContain('text-orange-600');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('claude-opus-4-8');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('abcd1234');
  });

  it('rate_limit → Checking messages badge + message preview', () => {
    row(ev('rate_limit', { message: 'slow down' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('slow down');
  });

  it('lifecycle / status_change fall into Checking messages + keep their preview', () => {
    row(ev('status_change', { from: 'queued', to: 'active' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('queued → active');
  });

  it('unknown event type → Checking messages badge + JSON preview', () => {
    row(ev('weird.event', { foo: 'bar' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('foo');
    // raw event_type is still recorded on the row for operators.
    expect(screen.getByTestId('agent-activity-row')).toHaveAttribute('data-event-type', 'weird.event');
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
