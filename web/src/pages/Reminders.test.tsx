import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { Reminder } from '@/api/reminders';

// T207 — unit test for the Reminders list page (screen ①). The data hooks are
// mocked so the test asserts the page's rendering + interaction logic
// (rows / stats / filters / row actions) deterministically.

const mutate = vi.fn();
let lastListParams: unknown = null;

const cron: Reminder = {
  id: 'rmd-1',
  organization_id: 'org-1',
  project_id: 'proj-1',
  creator_ref: 'user:pd',
  remindee_agent_id: 'agent-tester1',
  content: '跑一遍回归测试',
  status: 'active',
  skip_if_overlap: true,
  deliver_as_creator: true,
  fired_count: 0,
  version: 1,
  schedule: { kind: 'cron', cron_expr: '0 18 * * 1-5', timezone: 'Asia/Shanghai' },
  next_run_at: '2099-01-01T18:00:00Z',
  created_at: '2026-06-16T00:00:00Z',
  updated_at: '2026-06-16T00:00:00Z',
};
const once: Reminder = {
  ...cron,
  id: 'rmd-2',
  remindee_agent_id: 'agent-dev1',
  content: '推分支并回讨论串',
  status: 'paused',
  schedule: { kind: 'once', once_at: '2099-01-01T17:00:00Z' },
  next_run_at: null,
};

vi.mock('@/api/reminders', () => ({
  useReminders: (_slug: string | undefined, params: unknown) => {
    lastListParams = params;
    return { data: [cron, once], isLoading: false, isError: false };
  },
  useUpdateReminder: () => ({ mutate, isPending: false }),
}));
vi.mock('@/api/members', () => ({ useDisplayNameResolver: () => (ref: string) => ref }));
vi.mock('@/components/ReminderCreateModal', () => ({
  ReminderCreateModal: () => <div data-testid="reminder-create-modal" />,
}));
vi.mock('@/components/ReminderDetailModal', () => ({
  ReminderDetailModal: () => <div data-testid="reminder-detail-modal" />,
}));

import Reminders from './Reminders';

// T248: filters live in the URL query (driven by col② RemindersSecondaryNav);
// the page READS them. So tests drive filters via the initial query string.
function renderPage(query = '') {
  return render(
    <MemoryRouter initialEntries={[`/organizations/acme/reminders${query}`]}>
      <Routes>
        <Route path="/organizations/:slug/reminders" element={<Reminders />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Reminders page', () => {
  afterEach(() => {
    cleanup();
    mutate.mockReset();
    lastListParams = null;
  });

  it('renders the stats, both rows, and the kind badges', () => {
    renderPage();
    expect(screen.getByTestId('stat-active').textContent).toContain('1');
    expect(screen.getByTestId('stat-paused').textContent).toContain('1');
    expect(screen.getAllByTestId('reminder-row')).toHaveLength(2);
    expect(screen.getByText('Once')).toBeTruthy();
    expect(screen.getByText('Recurring')).toBeTruthy();
    // the paused row shows the "— Paused" next-trigger affordance.
    expect(screen.getByText('— Paused')).toBeTruthy();
  });

  it('pause action fires update.mutate with action=pause', () => {
    renderPage();
    fireEvent.click(screen.getAllByTestId('reminder-pause')[0]);
    expect(mutate).toHaveBeenCalledWith({ id: 'rmd-1', action: 'pause' });
  });

  it('the ?range=created query drives the list query filter', () => {
    renderPage('?range=created');
    expect(lastListParams).toMatchObject({ filter: 'created' });
  });

  it('the ?range=remindee query drives the remindee list query filter', () => {
    renderPage('?range=remindee');
    expect(lastListParams).toMatchObject({ filter: 'remindee' });
  });

  it('the ?status=active query narrows the query statuses', () => {
    renderPage('?status=active');
    expect(lastListParams).toMatchObject({ statuses: ['active'] });
  });

  it('defaults to filter=all and hides terminal reminders (statuses=active,paused) when the query is empty', () => {
    renderPage();
    expect(lastListParams).toMatchObject({ filter: 'all', statuses: ['active', 'paused'] });
  });

  it('the ?status=all query shows every status (statuses=undefined)', () => {
    renderPage('?status=all');
    expect(lastListParams).toMatchObject({ filter: 'all', statuses: undefined });
  });

  it('opens the create modal from the New reminder button', () => {
    renderPage();
    expect(screen.queryByTestId('reminder-create-modal')).toBeNull();
    fireEvent.click(screen.getByTestId('reminder-new'));
    expect(screen.getByTestId('reminder-create-modal')).toBeTruthy();
  });
});
