import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import type { IssueStatus, TaskStatus } from '@/api/types';
import { IssueTaskSidebar, StatusBlock } from './IssueTaskSidebar';

type SK = IssueStatus | TaskStatus;

// v2.8.1 #5th Phabricator Issue/Task refactor — shared sidebar + status大色块.
describe('IssueTaskSidebar + StatusBlock (#5th)', () => {
  afterEach(() => cleanup());

  it('maps every real Issue + Task status to its locked label + class (@oopslink FINAL: white-on-saturated)', () => {
    // @oopslink FINAL palette lock: bg-<color> text-white. verified=purple,
    // closed=cyan, discarded=rust-700, reopened=pink.
    const cases: Array<[SK, string, string]> = [
      ['open', 'Open', 'bg-slate-500 text-white'],
      ['in_progress', 'In Progress', 'bg-blue-500 text-white'],
      ['running', 'Running', 'bg-blue-500 text-white'],
      ['blocked', 'Blocked', 'bg-orange-500 text-white'],
      ['resolved', 'Resolved', 'bg-green-600 text-white'],
      ['completed', 'Completed', 'bg-green-600 text-white'],
      ['verified', 'Verified', 'bg-purple-600 text-white'],
      ['closed', 'Closed', 'bg-cyan-600 text-white'], // cyan (terminal Issue, distinct from open's slate)
      ['discarded', 'Discarded', 'bg-rust-700 text-white'], // deep-rust (terminal, replaces canceled/withdrawn)
      ['reopened', 'Reopened', 'bg-pink-600 text-white'],
    ];
    for (const [status, label, cls] of cases) {
      cleanup();
      render(<StatusBlock status={status} />);
      const block = screen.getByTestId('status-block');
      expect(block).toHaveTextContent(label);
      expect(block.className).toContain(cls);
      expect(block.getAttribute('data-status')).toBe(status);
    }
  });

  it('keeps verified (purple) and completed/resolved (green) in DISTINCT color families (a11y, not just label)', () => {
    render(<StatusBlock status="verified" />);
    expect(screen.getByTestId('status-block').className).toContain('purple');
    cleanup();
    render(<StatusBlock status="completed" />);
    expect(screen.getByTestId('status-block').className).toContain('green');
  });

  it('renders status block + actions + meta rows + (Task) WorkItems summary slots', () => {
    render(
      <IssueTaskSidebar
        status="running"
        actions={<button type="button">Edit</button>}
        meta={[{ label: 'Created by', value: 'alice', testId: 'meta-created-by' }]}
        workItemsSummary={<span>2 In Progress · 1 Paused</span>}
      />,
    );
    expect(screen.getByTestId('status-block')).toHaveTextContent('Running');
    expect(screen.getByTestId('sidebar-actions')).toHaveTextContent('Edit');
    expect(screen.getByTestId('meta-created-by')).toHaveTextContent('alice');
    // Paused is part of the Task WorkItems summary (cross-PR consistency with #199).
    expect(screen.getByTestId('sidebar-workitems-summary')).toHaveTextContent('2 In Progress · 1 Paused');
  });

  it('omits optional slots when not provided', () => {
    render(<IssueTaskSidebar status="open" />);
    expect(screen.getByTestId('status-block')).toBeInTheDocument();
    expect(screen.queryByTestId('sidebar-actions')).toBeNull();
    expect(screen.queryByTestId('sidebar-meta')).toBeNull();
    expect(screen.queryByTestId('sidebar-workitems-summary')).toBeNull();
  });
});
