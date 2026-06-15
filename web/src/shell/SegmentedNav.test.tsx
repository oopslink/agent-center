import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SegmentedNav, type Segment } from './SegmentedNav';

const ITEMS: ReadonlyArray<Segment> = [
  { label: 'Environment', to: '/environment', testId: 'seg-env' },
  { label: 'Settings', to: '/settings', testId: 'seg-set' },
];

function renderNav(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <SegmentedNav items={ITEMS} ariaLabel="System sections" />
    </MemoryRouter>,
  );
}

describe('SegmentedNav (v2.10.1 reusable mobile 二级段控)', () => {
  afterEach(() => cleanup());

  it('renders a segment per item linking to its org-relative route', () => {
    renderNav('/environment');
    const nav = screen.getByRole('navigation', { name: 'System sections' });
    expect(within(nav).getByTestId('seg-env')).toHaveAttribute('href', '/environment');
    expect(within(nav).getByTestId('seg-set')).toHaveAttribute('href', '/settings');
  });

  it('marks the segment matching the current route active (aria-current=page)', () => {
    renderNav('/settings');
    expect(screen.getByTestId('seg-set')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('seg-set')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('seg-env')).toHaveAttribute('data-active', 'false');
  });

  it('matches the active segment under an org-prefixed path', () => {
    renderNav('/organizations/acme/environment');
    expect(screen.getByTestId('seg-env')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('seg-set')).toHaveAttribute('data-active', 'false');
  });

  it('every segment is a ≥44px touch target', () => {
    renderNav('/environment');
    expect(screen.getByTestId('seg-env').className).toContain('min-h-[44px]');
    expect(screen.getByTestId('seg-set').className).toContain('min-h-[44px]');
  });
});
