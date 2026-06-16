import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import Settings from './Settings';

function wrap(path = '/settings') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Settings />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('Settings page', () => {
  // I7-D3: the Version panel moved out to its own /version page — Settings no
  // longer renders build identity (the guardrail params panel lands here once
  // D1's settings API is on main).
  it('no longer renders the version panel (moved to /version)', () => {
    wrap();
    expect(screen.queryByTestId('version-panel')).not.toBeInTheDocument();
  });

  // v2.10.1 [M7] the System module mobile 二级段控 (Environment | Settings | Version).
  it('renders the mobile System segmented nav with Settings active', async () => {
    wrap('/settings');
    const nav = await screen.findByTestId('segmented-nav');
    expect(within(nav).getByTestId('system-seg-settings')).toHaveAttribute('data-active', 'true');
    expect(within(nav).getByTestId('system-seg-environment')).toHaveAttribute('data-active', 'false');
    expect(within(nav).getByTestId('system-seg-version')).toHaveAttribute('data-active', 'false');
  });
});
