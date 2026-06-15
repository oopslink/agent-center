import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { MobileMetaSummary, MobileDetailsPanel } from './WorkItemMobileMeta';

// T145 — the mobile (<md) meta surfaces are JS-gated on matchMedia so they only
// mount on a phone (and never double-render the shared StatusBlock/EntityRef in
// the desktop test env). These tests stub matchMedia to opt into the mobile tree.

function setViewport(isMobile: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: isMobile,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }));
}

function render(ui: React.ReactElement) {
  return rtlRender(<MemoryRouter>{ui}</MemoryRouter>);
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('WorkItemMobileMeta (T145 mobile detail meta)', () => {
  it('does NOT render on desktop (matchMedia false) — avoids double meta', () => {
    setViewport(false);
    render(
      <MobileMetaSummary status="running" projectId="proj-a" assignee={null} />,
    );
    expect(screen.queryByTestId('wi-mobile-summary')).not.toBeInTheDocument();
  });

  it('MobileMetaSummary shows status + assignee + plan on mobile, ABOVE the fold', () => {
    setViewport(true);
    render(
      <MobileMetaSummary
        status="running"
        statusChangedAt={undefined}
        assignee="agent:builder"
        assigneeName="builder-bot"
        projectId="proj-a"
        plan={{ id: 'PL-1', name: 'Sprint 1' }}
      />,
    );
    const summary = screen.getByTestId('wi-mobile-summary');
    expect(summary).toBeInTheDocument();
    // status chip present (exactly one — the desktop sidebar isn't mounted here).
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'running');
    // assignee name + plan link.
    expect(screen.getByText('builder-bot')).toBeInTheDocument();
    const planLink = screen.getByTestId('wi-mobile-plan-link');
    expect(planLink).toHaveAttribute('href', '/projects/proj-a/plans/PL-1');
  });

  it('MobileMetaSummary omits the assignee row for an Issue (assignee undefined)', () => {
    setViewport(true);
    render(<MobileMetaSummary status="open" projectId="proj-a" />);
    expect(screen.getByTestId('wi-mobile-summary')).toBeInTheDocument();
    expect(screen.queryByTestId('wi-mobile-assignee-empty')).not.toBeInTheDocument();
    expect(screen.queryByTestId('wi-mobile-assignee-open')).not.toBeInTheDocument();
  });

  it('MobileMetaSummary shows "Unassigned" when assignee is null (Task, none set)', () => {
    setViewport(true);
    render(<MobileMetaSummary status="open" projectId="proj-a" assignee={null} />);
    expect(screen.getByTestId('wi-mobile-assignee-empty')).toHaveTextContent('Unassigned');
  });

  it('MobileDetailsPanel renders compact rows + a ≥44px Edit button; Edit fires onEdit', () => {
    setViewport(true);
    const onEdit = vi.fn();
    render(
      <MobileDetailsPanel
        kind="task"
        projectId="proj-a"
        projectName="Alpha"
        itemId="task-abcdef123456"
        orgRef="T7"
        createdAt="2026-06-01T00:00:00Z"
        tags={['backend', 'urgent']}
        editable
        onEdit={onEdit}
      />,
    );
    expect(screen.getByTestId('wi-mobile-details')).toBeInTheDocument();
    // single-line id pill (org_ref handle) + project link.
    expect(screen.getByTestId('wi-mobile-id-pill')).toHaveTextContent('T7');
    expect(screen.getByTestId('wi-mobile-project-link')).toHaveTextContent('Alpha');
    expect(screen.getAllByTestId('wi-mobile-tag-chip')).toHaveLength(2);
    // ≥44px touch targets (mobile UX standard).
    expect(screen.getByTestId('wi-mobile-details-summary').className).toContain('min-h-[2.75rem]');
    const edit = screen.getByTestId('wi-mobile-edit-button');
    expect(edit.className).toContain('min-h-[2.75rem]');
    fireEvent.click(edit);
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('MobileDetailsPanel hides the Edit button on a terminal item (editable=false)', () => {
    setViewport(true);
    render(
      <MobileDetailsPanel
        kind="issue"
        projectId="proj-a"
        itemId="issue-1"
        createdAt="2026-06-01T00:00:00Z"
        tags={[]}
        editable={false}
        onEdit={() => {}}
      />,
    );
    expect(screen.queryByTestId('wi-mobile-edit-button')).not.toBeInTheDocument();
  });
});
