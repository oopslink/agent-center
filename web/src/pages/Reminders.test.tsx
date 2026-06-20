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

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/organizations/acme/reminders']}>
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
    expect(screen.getByText('一次性')).toBeTruthy();
    expect(screen.getByText('周期')).toBeTruthy();
    // the paused row shows the "— 已暂停" next-trigger affordance.
    expect(screen.getByText('— 已暂停')).toBeTruthy();
  });

  it('pause action fires update.mutate with action=pause', () => {
    renderPage();
    fireEvent.click(screen.getAllByTestId('reminder-pause')[0]);
    expect(mutate).toHaveBeenCalledWith({ id: 'rmd-1', action: 'pause' });
  });

  it('the 我创建的 range filter drives the list query filter', () => {
    renderPage();
    fireEvent.click(screen.getByTestId('reminder-range-created'));
    expect(lastListParams).toMatchObject({ filter: 'created' });
  });

  it('the 提醒我的 range filter drives the remindee list query filter', () => {
    renderPage();
    fireEvent.click(screen.getByTestId('reminder-range-remindee'));
    expect(lastListParams).toMatchObject({ filter: 'remindee' });
  });

  it('the status filter narrows the query statuses', () => {
    renderPage();
    fireEvent.click(screen.getByTestId('reminder-status-active'));
    expect(lastListParams).toMatchObject({ statuses: ['active'] });
  });

  it('opens the create modal from the 新建提醒 button', () => {
    renderPage();
    expect(screen.queryByTestId('reminder-create-modal')).toBeNull();
    fireEvent.click(screen.getByTestId('reminder-new'));
    expect(screen.getByTestId('reminder-create-modal')).toBeTruthy();
  });
});
