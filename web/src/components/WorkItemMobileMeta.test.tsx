import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { MobileBannerMeta, MobileDetailsContent } from './WorkItemMobileMeta';

// T309 — MobileBannerMeta renders inline in the conversation banner on mobile
// (status + duration + Actions dropdown with Show info / Edit).

function render(ui: React.ReactElement) {
  return rtlRender(<MemoryRouter>{ui}</MemoryRouter>);
}

const noop = (): void => {};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('MobileBannerMeta (inline banner chips)', () => {
  it('shows status + Actions dropdown with Show info + Edit', () => {
    const onEdit = vi.fn();
    const onToggle = vi.fn();
    render(
      <MobileBannerMeta
        kind="task"
        status="running"
        showInfo={false}
        onToggleInfo={onToggle}
        editable
        onEdit={onEdit}
      />,
    );
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'running');
    // Open Actions dropdown to access Show info + Edit.
    fireEvent.click(screen.getByTestId('wi-mobile-actions-toggle'));
    fireEvent.click(screen.getByTestId('wi-mobile-showinfo'));
    expect(onToggle).toHaveBeenCalledTimes(1);
    // Re-open (dropdown closes after action).
    fireEvent.click(screen.getByTestId('wi-mobile-actions-toggle'));
    fireEvent.click(screen.getByTestId('wi-mobile-edit-button'));
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('reflects the showInfo state on the toggle label + aria-expanded', () => {
    render(
      <MobileBannerMeta kind="task" status="open" showInfo onToggleInfo={noop} editable onEdit={noop} />,
    );
    // Open Actions dropdown to see Show info toggle.
    fireEvent.click(screen.getByTestId('wi-mobile-actions-toggle'));
    const toggle = screen.getByTestId('wi-mobile-showinfo');
    expect(toggle).toHaveTextContent('Hide info');
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });

  it('hides the Edit button on a terminal item (editable=false)', () => {
    render(
      <MobileBannerMeta kind="task" status="discarded" showInfo={false} onToggleInfo={noop} editable={false} onEdit={noop} />,
    );
    fireEvent.click(screen.getByTestId('wi-mobile-actions-toggle'));
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
