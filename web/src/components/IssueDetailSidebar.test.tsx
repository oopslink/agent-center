import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Issue } from '@/api/types';
import { IssueDetailSidebar } from './IssueDetailSidebar';

// v2.8.1 sidebar-align: IssueDetailSidebar mirrors TaskDetailSidebar's two-section
// layout (Details header + Edit-Issue pencil · Status[block+duration] · Tags /
// divider / Project · Issue ID · Created) — MINUS assignee (Issues have none),
// edit-modal-only.

function makeIssue(over: Partial<Issue> = {}): Issue {
  return {
    id: 'issue-01KT8DABCDEF',
    project_id: 'proj-a',
    title: 'login bug',
    description: 'cannot sign in',
    status: 'open',
    created_by: 'user:hayang',
    tags: [],
    version: 1,
    created_at: '2026-05-24T01:00:00Z',
    updated_at: '2026-05-24T01:00:00Z',
    ...over,
  };
}

function renderSidebar(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('IssueDetailSidebar (v2.8.1 sidebar-align)', () => {
  afterEach(() => cleanup());

  it('renders the two-section layout: Details header + Status + Tags / Project + Issue ID + Created', () => {
    renderSidebar(
      <IssueDetailSidebar
        issue={makeIssue({ tags: ['alpha', 'beta'], org_ref: 'I42' })}
        projectName="Project A"
        onEdit={() => {}}
        editable
      />,
    );
    // Top section: "Details" header + compact StatusBlock under "Status".
    expect(screen.getByText('Details')).toBeInTheDocument();
    expect(screen.getByText('Status')).toBeInTheDocument();
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'open');
    // Tags read-only chips.
    const chips = screen.getAllByTestId('issue-tag-chip');
    expect(chips.map((c) => c.getAttribute('data-tag'))).toEqual(['alpha', 'beta']);
    // Bottom read-only section: Project link, Issue ID pill (org_ref), Created.
    expect(screen.getByTestId('issue-project-link')).toHaveAttribute('href', '/projects/proj-a');
    expect(screen.getByTestId('issue-project-link')).toHaveTextContent('Project A');
    expect(screen.getByText('Issue ID')).toBeInTheDocument();
    expect(screen.getByTestId('issue-id-pill')).toHaveTextContent('I42');
    expect(screen.getByTestId('issue-created')).toBeInTheDocument();
  });

  it('has NO assignee section (Issues have no assignee)', () => {
    renderSidebar(<IssueDetailSidebar issue={makeIssue()} onEdit={() => {}} editable />);
    expect(screen.queryByText('Assignee')).toBeNull();
    expect(screen.queryByTestId('issue-sidebar-assignee')).toBeNull();
  });

  it('Edit-Issue pencil button (aria-label) calls onEdit — the sole edit path', () => {
    const onEdit = vi.fn();
    renderSidebar(<IssueDetailSidebar issue={makeIssue()} onEdit={onEdit} editable />);
    const btn = screen.getByTestId('issue-edit-button');
    expect(btn).toHaveAttribute('aria-label', 'Edit issue');
    expect(btn).toHaveTextContent('Edit Issue');
    fireEvent.click(btn);
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('hides the Edit-Issue button when not editable (terminal issue)', () => {
    renderSidebar(
      <IssueDetailSidebar issue={makeIssue({ status: 'discarded' })} onEdit={() => {}} editable={false} />,
    );
    expect(screen.queryByTestId('issue-edit-button')).toBeNull();
    // Status still shown read-only.
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'discarded');
  });

  it('shows the in-status duration next to the StatusBlock when status_changed_at is set', () => {
    renderSidebar(
      <IssueDetailSidebar
        issue={makeIssue({ status_changed_at: '2000-01-01T00:00:00Z' })}
        onEdit={() => {}}
        editable
      />,
    );
    // Long ago → a "<n>d ..." duration string is rendered.
    expect(screen.getByTestId('issue-status-duration')).toBeInTheDocument();
  });

  it('falls back to the FULL id (never a #id-tail hash) when org_ref is absent (T126)', () => {
    renderSidebar(<IssueDetailSidebar issue={makeIssue({ org_ref: undefined })} onEdit={() => {}} editable />);
    // T126: no org_ref → the full id is shown verbatim, NOT a 6-char hash.
    expect(screen.getByTestId('issue-id-pill')).toHaveTextContent('issue-01KT8DABCDEF');
    expect(screen.getByTestId('issue-id-pill')).not.toHaveTextContent('#');
  });
});
