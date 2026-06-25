import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { UseQueryResult } from '@tanstack/react-query';
import { OrgGuard, OrgRedirect } from './OrgContext';
import type { OrgResult } from './api/auth';

// Mock useOrgs so each test drives a precise React Query state
// (isLoading / isError / isSuccess + data). This isolates OrgGuard /
// OrgRedirect's redirect-gating logic from network + retry timing.
vi.mock('./api/auth', () => ({
  useOrgs: vi.fn(),
}));

import { useOrgs } from './api/auth';

const mockUseOrgs = vi.mocked(useOrgs);

type OrgsQuery = Pick<
  UseQueryResult<OrgResult[]>,
  'data' | 'isLoading' | 'isError' | 'isSuccess'
>;

function setOrgsState(state: OrgsQuery): void {
  mockUseOrgs.mockReturnValue(state as unknown as UseQueryResult<OrgResult[]>);
}

const ORG: OrgResult = {
  id: 'org-1',
  slug: 'acme',
  name: 'Acme',
  created_at: '2026-01-01T00:00:00Z',
};

// loading: neither settled nor errored.
const LOADING: OrgsQuery = { data: undefined, isLoading: true, isError: false, isSuccess: false };
// transient error: not loading, errored, NOT yet successfully settled (the
// starvation-401 case React Query is still retrying / has just failed).
const TRANSIENT_ERROR: OrgsQuery = { data: undefined, isLoading: false, isError: true, isSuccess: false };
const SUCCESS_EMPTY: OrgsQuery = { data: [], isLoading: false, isError: false, isSuccess: true };
const SUCCESS_WITH_ORG: OrgsQuery = { data: [ORG], isLoading: false, isError: false, isSuccess: true };

// Render OrgGuard under a router with a /signup sink + an org-scoped route, so
// a premature redirect surfaces as the signup marker instead of guard content.
function renderGuard(slug: string) {
  return render(
    <MemoryRouter initialEntries={[`/organizations/${slug}`]}>
      <Routes>
        <Route
          path="/organizations/:slug"
          element={
            <OrgGuard>
              <div data-testid="guard-children">protected</div>
            </OrgGuard>
          }
        />
        <Route path="/signup" element={<div data-testid="signup-page">signup</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderRedirect() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={<OrgRedirect />} />
        <Route path="/signup" element={<div data-testid="signup-page">signup</div>} />
        <Route path="/organizations/:slug" element={<div data-testid="org-home">org home</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('OrgGuard redirect gating (v2.9 401-retry)', () => {
  it('shows the spinner while loading (no premature /signup)', () => {
    setOrgsState(LOADING);
    renderGuard('acme');
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
    expect(screen.queryByTestId('guard-children')).not.toBeInTheDocument();
  });

  it('on a transient error (not yet settled) shows the spinner, NOT a /signup redirect', () => {
    // The core bug: pre-fix, isError + data===undefined → (data ?? []).length===0
    // → premature Navigate to /signup. The fix gates the redirect on isSuccess,
    // so a transient error keeps the spinner up while React Query retries.
    setOrgsState(TRANSIENT_ERROR);
    renderGuard('acme');
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
    expect(screen.queryByTestId('guard-children')).not.toBeInTheDocument();
  });

  it('transient error then success → renders org context (no /signup)', () => {
    // First render: transient error → spinner.
    setOrgsState(TRANSIENT_ERROR);
    const { rerender } = renderGuard('acme');
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();

    // Retry succeeds → children render, still no /signup bounce.
    setOrgsState(SUCCESS_WITH_ORG);
    rerender(
      <MemoryRouter initialEntries={['/organizations/acme']}>
        <Routes>
          <Route
            path="/organizations/:slug"
            element={
              <OrgGuard>
                <div data-testid="guard-children">protected</div>
              </OrgGuard>
            }
          />
          <Route path="/signup" element={<div data-testid="signup-page">signup</div>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByTestId('guard-children')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
  });

  it('settled success with no orgs → redirects to /signup', () => {
    setOrgsState(SUCCESS_EMPTY);
    renderGuard('acme');
    expect(screen.getByTestId('signup-page')).toBeInTheDocument();
    expect(screen.queryByTestId('guard-children')).not.toBeInTheDocument();
  });

  it('settled success with a matching slug → renders children/context', () => {
    setOrgsState(SUCCESS_WITH_ORG);
    renderGuard('acme');
    expect(screen.getByTestId('guard-children')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
  });

  it('settled success but slug not in orgs → OrgErrorScreen 404 (not /signup)', () => {
    setOrgsState(SUCCESS_WITH_ORG);
    renderGuard('other-org');
    expect(screen.getByTestId('org-error')).toBeInTheDocument();
    expect(screen.getByText('Organization not found or no access')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
  });

  // T478 (Option A): a disabled org now stays in the member's list.
  it('disabled org + non-owner member → clear "disabled" screen (not 404, not children)', () => {
    setOrgsState({
      data: [{ ...ORG, disabled: true, role: 'member' }],
      isLoading: false,
      isError: false,
      isSuccess: true,
    });
    renderGuard('acme');
    expect(screen.getByTestId('org-disabled')).toBeInTheDocument();
    expect(screen.getByText('This organization is disabled')).toBeInTheDocument();
    // Not the misleading 404, and the protected children never mount.
    expect(screen.queryByTestId('org-error')).not.toBeInTheDocument();
    expect(screen.queryByTestId('guard-children')).not.toBeInTheDocument();
  });

  it('disabled org + OWNER → full access (children render so they can re-enable)', () => {
    setOrgsState({
      data: [{ ...ORG, disabled: true, role: 'owner' }],
      isLoading: false,
      isError: false,
      isSuccess: true,
    });
    renderGuard('acme');
    expect(screen.getByTestId('guard-children')).toBeInTheDocument();
    expect(screen.queryByTestId('org-disabled')).not.toBeInTheDocument();
  });
});

describe('OrgRedirect redirect gating (v2.9 401-retry)', () => {
  it('shows the spinner while loading (no premature /signup)', () => {
    setOrgsState(LOADING);
    renderRedirect();
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
  });

  it('on a transient error shows the spinner, NOT /signup', () => {
    setOrgsState(TRANSIENT_ERROR);
    renderRedirect();
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
    expect(screen.queryByTestId('org-home')).not.toBeInTheDocument();
  });

  it('settled success with a first org → redirects to that org home', () => {
    setOrgsState(SUCCESS_WITH_ORG);
    renderRedirect();
    expect(screen.getByTestId('org-home')).toBeInTheDocument();
    expect(screen.queryByTestId('signup-page')).not.toBeInTheDocument();
  });

  it('settled success with no orgs → redirects to /signup', () => {
    setOrgsState(SUCCESS_EMPTY);
    renderRedirect();
    expect(screen.getByTestId('signup-page')).toBeInTheDocument();
    expect(screen.queryByTestId('org-home')).not.toBeInTheDocument();
  });
});
