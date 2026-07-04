import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
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

// The expanded detail now linkifies entity ids via ActivityRefText, whose agent
// resolver always loads the org members list. Provide an empty default so the
// non-linkify tests don't trip onUnhandledRequest:'error'; the task/plan/issue
// resolvers stay disabled here (no OrgContext → no slug → no fetch).
beforeEach(() => {
  server.use(http.get('/api/members', () => HttpResponse.json([])));
});

function row(e: AgentActivityEvent) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <table>
        <tbody>
          <AgentActivityRow event={e} />
        </tbody>
      </table>
    </QueryClientProvider>,
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

  // v2.8 #274 increment 4: tool_use → CAT_TOOL_USE (replaces Running command /
  // Searching code, Q1) — a "Running"/"Searching" label + an SVG icon (NOT emoji,
  // Q2 search-vs-run distinction kept for the icon + sub-label).
  it('tool_use (non-search) → "Running" badge + wrench SVG icon + ToolName(args)', () => {
    row(ev('tool_use', { tool_name: 'run_shell', args: { cmd: 'ls' } }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Running');
    expect(badge.className).toContain('text-brand');
    // SVG icon component, never an emoji character (ux-standards).
    expect(badge.querySelector('svg')).toBeInTheDocument();
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('run_shell(');
  });

  it('tool_use (search tool Grep) → "Searching" badge + magnifier SVG icon', () => {
    row(ev('tool_use', { tool_name: 'Grep', args: { pattern: 'foo' } }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Searching');
    expect(badge.className).toContain('text-status-purple-strong');
    expect(badge.querySelector('svg')).toBeInTheDocument();
  });

  it('tool_use Read → "Searching" (allowlist is case-insensitive)', () => {
    row(ev('tool_use', { tool_name: 'Read', args: { path: '/x' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching');
  });

  it('tool_use WebSearch / WebFetch → "Searching"', () => {
    row(ev('tool_use', { tool_name: 'WebSearch', args: { query: 'foo' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching');
    cleanup();
    row(ev('tool_use', { tool_name: 'WebFetch', args: { url: 'http://x' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Searching');
  });

  it('Bash shell search (rg/find) is NOT a search tool — tool_name=Bash → "Running"', () => {
    row(ev('tool_use', { tool_name: 'Bash', args: { command: 'rg foo' } }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Running');
  });

  // tool_result → CAT_TOOL_RESULT with a ✓/✗ SVG status icon from payload.ok (Q3).
  it('failed tool_result → "Result" badge + ✗ status (error) + SVG icon + failed marker', () => {
    row(ev('tool_result', { tool_name: 'run', duration_ms: 120, tokens: 50, ok: false }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Result');
    expect(badge).toHaveAttribute('data-tool-status', 'error');
    expect(badge.className).toContain('text-danger');
    expect(badge.querySelector('svg')).toBeInTheDocument(); // ✗ SVG, not emoji
    expect(badge).toHaveAttribute('aria-label', 'Result, error');
    expect(screen.getByTestId('agent-activity-failed')).toHaveTextContent('failed');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('120ms');
  });

  it('successful tool_result → "Result" badge + ✓ status (ok) + no failed marker', () => {
    row(ev('tool_result', { tool_name: 'run', duration_ms: 5, ok: true }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Result');
    expect(badge).toHaveAttribute('data-tool-status', 'ok');
    expect(badge).toHaveAttribute('aria-label', 'Result, ok');
    expect(screen.queryByTestId('agent-activity-failed')).not.toBeInTheDocument();
  });

  // #274: tool_result output renders through the shared CollapsibleCodeBlock.
  it('tool_result expands to its output via CollapsibleCodeBlock (.content)', () => {
    const longOut = Array.from({ length: 25 }, (_, i) => `out ${i + 1}`).join('\n');
    row(ev('tool_result', { tool_name: 'run', ok: true, tool_result: { content: longOut } }));
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    const out = screen.getByTestId('agent-activity-tool-output');
    expect(out.querySelector('[data-testid="collapsible-code-block"]')).toBeInTheDocument();
    // long output (>20 lines) → collapsed with the disclosure.
    expect(screen.getByTestId('code-disclosure-btn')).toBeInTheDocument();
  });

  it('tool_result with no .content falls back to pretty-printed JSON (Lock 12)', () => {
    row(ev('tool_result', { tool_name: 'run', ok: true, tool_result: { count: 5 } }));
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    expect(screen.getByTestId('agent-activity-tool-output')).toHaveTextContent('"count": 5');
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
    expect(badge.className).toContain('text-status-orange-strong');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('claude-opus-4-8');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('abcd1234');
  });

  it('rate_limit → Checking messages badge + message preview', () => {
    row(ev('rate_limit', { message: 'slow down' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('slow down');
  });

  it('status_change falls into Checking messages + keeps its preview', () => {
    row(ev('status_change', { from: 'queued', to: 'active' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('queued → active');
  });

  // T345: lifecycle ops get their OWN "Control" category (not folded into Checking).
  it('lifecycle → Control badge + verb preview (started/stopped/restarted/reset)', () => {
    row(ev('lifecycle', { event: 'restarted' }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Control');
    expect(badge).not.toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('restarted');
  });

  it('lifecycle reset shows the scope in the preview', () => {
    row(ev('lifecycle', { event: 'reset', scope: 'workspace' }));
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('reset (workspace)');
  });

  // v2.31.0 (oopslink 2026-07-03): forked-executor lifecycle events split off from
  // Control into their OWN "Executor" category, and the preview surfaces which
  // executor is running which task without expanding the row.
  it('executor.start lifecycle → Executor badge (not Control) + exec/task/model preview', () => {
    row(
      ev('lifecycle', {
        event: 'executor.start',
        executor_id: 'exec-abcdef123456',
        task_ref: 'task-99',
        title: 'repo task',
        model: 'claude-opus',
        scope: 'claude-opus',
      }),
    );
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Executor');
    expect(badge).not.toHaveTextContent('Control');
    const preview = screen.getByTestId('agent-activity-preview');
    expect(preview).toHaveTextContent('start');
    expect(preview).toHaveTextContent('repo task'); // task visible without expanding
    expect(preview).toHaveTextContent('claude-opus');
  });

  it('executor.stop lifecycle → Executor badge + outcome in preview', () => {
    row(
      ev('lifecycle', {
        event: 'executor.stop',
        executor_id: 'exec-77',
        task_ref: 'task-99',
        outcome: 'failed',
        scope: 'failed:stalled',
      }),
    );
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Executor');
    const preview = screen.getByTestId('agent-activity-preview');
    expect(preview).toHaveTextContent('stop');
    expect(preview).toHaveTextContent('failed:stalled');
    expect(preview).toHaveTextContent('task-99');
  });

  it('executor.progress lifecycle → Executor badge + state in preview', () => {
    row(ev('lifecycle', { event: 'executor.progress', executor_id: 'exec-5', task_ref: 'task-1', state: 'running', scope: 'running' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Executor');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('progress');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('running');
  });

  it('non-executor lifecycle still maps to Control (session control ops unchanged)', () => {
    row(ev('lifecycle', { event: 'stopped' }));
    const badge = screen.getByTestId('agent-activity-badge');
    expect(badge).toHaveTextContent('Control');
    expect(badge).not.toHaveTextContent('Executor');
  });

  it('unknown event type → Checking messages badge + JSON preview', () => {
    row(ev('weird.event', { foo: 'bar' }));
    expect(screen.getByTestId('agent-activity-badge')).toHaveTextContent('Checking messages');
    expect(screen.getByTestId('agent-activity-preview')).toHaveTextContent('foo');
    // raw event_type is still recorded on the row for operators.
    expect(screen.getByTestId('agent-activity-row')).toHaveAttribute('data-event-type', 'weird.event');
  });

  it('toggles an expanded JSON payload (debug view) + shows refs', () => {
    row(ev('tool_use', { tool_name: 'read_file' }, { task_ref: 'pm://tasks/TS-1', interaction_ref: 'int-9' }));
    expect(screen.queryByTestId('agent-activity-payload-json')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    expect(screen.getByTestId('agent-activity-payload-json')).toHaveTextContent('read_file');
    expect(screen.getByTestId('agent-activity-task-ref')).toHaveTextContent('pm://tasks/TS-1');
    expect(screen.getByTestId('agent-activity-interaction-ref')).toHaveTextContent('int-9');
    // collapse again
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    expect(screen.queryByTestId('agent-activity-payload-json')).not.toBeInTheDocument();
  });

  it('survives malformed JSON payload without crashing', () => {
    row(ev('assistant_text', 'not json {'));
    expect(screen.getByTestId('agent-activity-row')).toBeInTheDocument();
  });

  // T500: message_delivered / message_acknowledged — two new event types for the
  // message-consumption activity feature (docs/design/features/agent-message-consumption-activity.md).
  it('renders message_delivered as a Received row with sender + preview', () => {
    row(
      ev('message_delivered', {
        conversation_id: 'c1',
        message_id: 'm1',
        sender_display: 'Alice',
        content_preview: 'hello there',
      }),
    );
    expect(screen.getByText(/Received/i)).toBeInTheDocument();
    expect(screen.getByText(/hello there/)).toBeInTheDocument();
  });

  it('renders message_acknowledged as an Acknowledged row', () => {
    row(ev('message_acknowledged', { conversation_id: 'c1', message_id: 'm9' }));
    expect(screen.getByText(/Acknowledged/i)).toBeInTheDocument();
  });
});

// oopslink DM 2026-07-04: the expanded activity detail must render entity ids as
// ref links (the screenshot case: an executor.progress event whose task_ref is a
// bare `task-<id>`). This asserts the AgentActivityRow ↔ ActivityRefText wiring
// end-to-end: within an org, a known task-<id> in BOTH the structured task field
// AND the payload JSON becomes a link to the task detail page.
describe('AgentActivityRow ref-link wiring (oopslink 2026-07-04)', () => {
  afterEach(() => cleanup());

  function rowInOrg(e: AgentActivityEvent, slug = 'test-org') {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <OrgContext.Provider value={{ slug, orgId: 'O', orgName: 'Test Org' }}>
          <table>
            <tbody>
              <AgentActivityRow event={e} />
            </tbody>
          </table>
        </OrgContext.Provider>
      </QueryClientProvider>,
    );
  }

  it('linkifies a bare task-<id> in the task field AND the payload JSON to the task detail page', async () => {
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [
            {
              id: 'task-5779df52', org_ref: 'T77', project: { id: 'proj-x', name: 'X' },
              title: 'the running task', status: 'running', assignee: null,
              updated_at: 'x', created_at: 'x',
            },
          ],
          total: 1,
        }),
      ),
    );
    // Mirrors the screenshot's executor.progress event: task_ref both as the
    // structured field AND inside the payload.
    rowInOrg(
      ev(
        'lifecycle',
        { event: 'executor.progress', executor_id: 'exec-86303eb9', state: 'running', task_ref: 'task-5779df52' },
        { task_ref: 'task-5779df52', interaction_ref: 'executor:exec-86303eb9' },
      ),
    );
    fireEvent.click(screen.getByTestId('agent-activity-toggle'));
    // Both the structured task field and the JSON payload resolve to task links.
    const links = await screen.findAllByTestId('activity-task-ref-link');
    expect(links.length).toBeGreaterThanOrEqual(2);
    for (const link of links) {
      expect(link.tagName).toBe('A');
      expect(link).toHaveTextContent('task-5779df52'); // literal id preserved (debug-faithful)
      expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-5779df52');
      expect(link).toHaveAttribute('target', '_blank');
    }
    // exec-<id> has no detail page → stays plain text (never a dangling link).
    expect(screen.queryByTestId('activity-agent-ref-link')).toBeNull();
    expect(screen.getByTestId('agent-activity-payload-json')).toHaveTextContent('exec-86303eb9');
  });
});
