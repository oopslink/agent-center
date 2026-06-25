import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { Reminder } from '@/api/reminders';

// T207 — unit test for the Reminders list page (screen ①). The data hooks are
// mocked so the test asserts the page's rendering + interaction logic
// (rows / stats / filters / row actions) deterministically.

const mutate = vi.fn();
const deleteMutate = vi.fn();
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
    return { data: { items: [cron, once], total: 2 }, isLoading: false, isError: false };
  },
  useUpdateReminder: () => ({ mutate, isPending: false }),
  useDeleteReminder: () => ({ mutate: deleteMutate, isPending: false, isError: false, error: null, reset: vi.fn() }),
}));
vi.mock('@/api/members', () => ({ useDisplayNameResolver: () => (ref: string) => ref }));
// Mock the create modal but echo back the props the page passes (editId / whether
// a prefill was supplied) so clone-vs-edit wiring is assertable.
vi.mock('@/components/ReminderCreateModal', () => ({
  ReminderCreateModal: (props: { editId?: string; prefill?: unknown }) => (
    <div
      data-testid="reminder-create-modal"
      data-edit-id={props.editId ?? ''}
      data-has-prefill={props.prefill ? 'true' : 'false'}
    />
  ),
  reminderToPrefill: (r: Reminder) => ({ remindeeIds: [r.remindee_agent_id] }),
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
    deleteMutate.mockReset();
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

  it('in-page status chips drive the same status filter (mobile-reachable)', () => {
    renderPage();
    // default chip "Active & Paused" is present (status filter reachable on the page).
    expect(screen.getByTestId('reminder-statuschip-default')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('reminder-statuschip-completed'));
    expect(lastListParams).toMatchObject({ statuses: ['completed'] });
    fireEvent.click(screen.getByTestId('reminder-statuschip-all'));
    expect(lastListParams).toMatchObject({ statuses: undefined });
  });

  it('opens the create modal from the New reminder button', () => {
    renderPage();
    expect(screen.queryByTestId('reminder-create-modal')).toBeNull();
    fireEvent.click(screen.getByTestId('reminder-new'));
    expect(screen.getByTestId('reminder-create-modal')).toBeTruthy();
  });

  // T477 — entry management: edit / clone / delete.
  it('clone opens the create modal WITH a prefill and NO editId (creates new)', () => {
    renderPage();
    fireEvent.click(screen.getAllByTestId('reminder-clone')[0]);
    const modal = screen.getByTestId('reminder-create-modal');
    expect(modal.getAttribute('data-has-prefill')).toBe('true');
    expect(modal.getAttribute('data-edit-id')).toBe('');
  });

  it('edit opens the create modal in edit mode (editId set + prefill)', () => {
    renderPage();
    // edit is shown only for active/paused rows; rmd-1 (active) has it.
    fireEvent.click(screen.getAllByTestId('reminder-edit')[0]);
    const modal = screen.getByTestId('reminder-create-modal');
    expect(modal.getAttribute('data-edit-id')).toBe('rmd-1');
    expect(modal.getAttribute('data-has-prefill')).toBe('true');
  });

  it('delete asks for confirmation, then fires useDeleteReminder with the row id', () => {
    renderPage();
    expect(screen.queryByTestId('reminder-delete-confirm')).toBeNull();
    fireEvent.click(screen.getAllByTestId('reminder-delete')[0]);
    // Confirm dialog appears; nothing deleted until confirmed.
    expect(screen.getByTestId('reminder-delete-confirm')).toBeInTheDocument();
    expect(deleteMutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId('reminder-delete-confirm-btn'));
    expect(deleteMutate).toHaveBeenCalledWith('rmd-1', expect.any(Object));
  });

  it('delete is available on EVERY row (incl. terminal) but edit only on active/paused', () => {
    renderPage();
    // two rows, both deletable; only the active row is editable (paused IS editable
    // too — both rmd-1 active and rmd-2 paused qualify → 2 edit buttons here).
    expect(screen.getAllByTestId('reminder-delete')).toHaveLength(2);
    expect(screen.getAllByTestId('reminder-clone')).toHaveLength(2);
    expect(screen.getAllByTestId('reminder-edit')).toHaveLength(2);
  });
});
