import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { MobileWorkItemBar, MobileDetailsContent } from './WorkItemMobileMeta';

// T309 — the mobile (<md) detail surfaces: a compact MobileWorkItemBar (status +
// assignee + Show info + Edit) and the MobileDetailsContent rows shown inside the
// "Show info" panel. The bar is JS-gated on matchMedia so it only mounts on a
// phone (and never double-renders the shared StatusBlock in the desktop env).

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

const noop = (): void => {};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('MobileWorkItemBar (T309 mobile detail bar)', () => {
  it('does NOT render on desktop (matchMedia false)', () => {
    setViewport(false);
    render(
      <MobileWorkItemBar kind="task" status="running" assignee={null} showInfo={false} onToggleInfo={noop} editable onEdit={noop} />,
    );
    expect(screen.queryByTestId('wi-mobile-bar')).not.toBeInTheDocument();
  });

  it('shows status + assignee + Show info + Edit on mobile', () => {
    setViewport(true);
    const onEdit = vi.fn();
    const onToggle = vi.fn();
    render(
      <MobileWorkItemBar
        kind="task"
        status="running"
        assignee="agent:builder"
        assigneeName="builder-bot"
        showInfo={false}
        onToggleInfo={onToggle}
        editable
        onEdit={onEdit}
      />,
    );
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'running');
    expect(screen.getByText('builder-bot')).toBeInTheDocument();
    // Show info toggles via the callback; Edit fires onEdit.
    fireEvent.click(screen.getByTestId('wi-mobile-showinfo'));
    expect(onToggle).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId('wi-mobile-edit-button'));
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('reflects the showInfo state on the toggle label + aria-expanded', () => {
    setViewport(true);
    render(
      <MobileWorkItemBar kind="task" status="open" assignee={null} showInfo onToggleInfo={noop} editable onEdit={noop} />,
    );
    const toggle = screen.getByTestId('wi-mobile-showinfo');
    expect(toggle).toHaveTextContent('Hide info');
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });

  it('shows "Unassigned" for a task with no assignee; omits the assignee for an Issue', () => {
    setViewport(true);
    const { rerender } = render(
      <MobileWorkItemBar kind="task" status="open" assignee={null} showInfo={false} onToggleInfo={noop} editable onEdit={noop} />,
    );
    expect(screen.getByTestId('wi-mobile-assignee-empty')).toHaveTextContent('Unassigned');
    // assignee undefined (Issue) → no assignee chip at all.
    rerender(
      <MemoryRouter>
        <MobileWorkItemBar kind="issue" status="open" showInfo={false} onToggleInfo={noop} editable onEdit={noop} />
      </MemoryRouter>,
    );
    expect(screen.queryByTestId('wi-mobile-assignee-empty')).not.toBeInTheDocument();
  });

  it('hides the Edit button on a terminal item (editable=false)', () => {
    setViewport(true);
    render(
      <MobileWorkItemBar kind="task" status="discarded" assignee={null} showInfo={false} onToggleInfo={noop} editable={false} onEdit={noop} />,
    );
    expect(screen.queryByTestId('wi-mobile-edit-button')).not.toBeInTheDocument();
  });
});

describe('MobileDetailsContent (T309 info-panel rows)', () => {
  it('renders project link + id pill (org_ref) + tags', () => {
    render(
      <MobileDetailsContent
        kind="task"
        projectId="proj-a"
        projectName="Alpha"
        itemId="task-abcdef123456"
        orgRef="T7"
        createdAt="2026-06-01T00:00:00Z"
        tags={['backend', 'urgent']}
      />,
    );
    expect(screen.getByTestId('wi-mobile-id-pill')).toHaveTextContent('T7');
    expect(screen.getByTestId('wi-mobile-project-link')).toHaveTextContent('Alpha');
    expect(screen.getAllByTestId('wi-mobile-tag-chip')).toHaveLength(2);
  });
});
