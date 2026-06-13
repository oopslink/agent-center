import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import type { IssueStatus, TaskStatus } from '@/api/types';
import { IssueTaskSidebar, StatusBlock } from './IssueTaskSidebar';

type SK = IssueStatus | TaskStatus;

// v2.8.1 #5th Phabricator Issue/Task refactor — shared sidebar + status大色块.
describe('IssueTaskSidebar + StatusBlock (#5th)', () => {
  afterEach(() => cleanup());

  it('maps every real Issue + Task status to its locked label + class (@oopslink REVISION 4: white-on-saturated)', () => {
    // @oopslink REVISION 4 palette lock: bg-<color> text-white. open=sky,
    // in_progress/running=blue-600, closed=slate-500, discarded=zinc-700,
    // reopened=amber. ADR-0046: blocked + verified are no longer statuses.
    const cases: Array<[SK, string, string]> = [
      ['open', 'Open', 'bg-status-sky-solid text-white'],
      ['in_progress', 'In Progress', 'bg-status-blue-solid text-white'],
      ['running', 'Running', 'bg-status-blue-solid text-white'],
      ['resolved', 'Resolved', 'bg-status-green-solid text-white'],
      ['completed', 'Completed', 'bg-status-green-solid text-white'],
      ['closed', 'Closed', 'bg-status-slate-solid text-white'], // slate (terminal Issue)
      ['discarded', 'Discarded', 'bg-status-zinc-solid text-white'], // zinc (terminal, replaces canceled/withdrawn)
      ['reopened', 'Reopened', 'bg-status-amber-solid text-white'],
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
