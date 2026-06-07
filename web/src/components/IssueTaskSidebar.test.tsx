import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import type { IssueStatus, TaskStatus } from '@/api/types';
import { IssueTaskSidebar, StatusBlock } from './IssueTaskSidebar';

type SK = IssueStatus | TaskStatus;

// v2.8.1 #5th Phabricator Issue/Task refactor — shared sidebar + status大色块.
describe('IssueTaskSidebar + StatusBlock (#5th)', () => {
  afterEach(() => cleanup());

  it('maps every real Issue + Task status to its locked label + color family (StatusChip-aligned)', () => {
    // blocked=orange / reopened=purple align StatusChip (#258); verified=teal (a11y-distinct from done green).
    const cases: Array<[SK, string, string]> = [
      ['open', 'Open', 'bg-slate-100'],
      ['in_progress', 'In Progress', 'bg-blue-100'],
      ['assigned', 'Assigned', 'bg-blue-100'],
      ['running', 'Running', 'bg-blue-100'],
      ['blocked', 'Blocked', 'bg-orange-100'],
      ['resolved', 'Resolved', 'bg-green-100'],
      ['completed', 'Completed', 'bg-green-100'],
      ['verified', 'Verified', 'bg-teal-100'],
      ['closed', 'Closed', 'bg-slate-100'],
      ['canceled', 'Canceled', 'bg-slate-100'],
      ['withdrawn', 'Withdrawn', 'bg-slate-100'], // slate (terminal, StatusChip-aligned; not red)
      ['reopened', 'Reopened', 'bg-purple-100'],
    ];
    for (const [status, label, bg] of cases) {
      cleanup();
      render(<StatusBlock status={status} />);
      const block = screen.getByTestId('status-block');
      expect(block).toHaveTextContent(label);
      expect(block.className).toContain(bg);
      expect(block.getAttribute('data-status')).toBe(status);
    }
  });

  it('keeps verified (teal) and completed/resolved (green) in DISTINCT color families (a11y, not just label)', () => {
    render(<StatusBlock status="verified" />);
    expect(screen.getByTestId('status-block').className).toContain('teal');
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
