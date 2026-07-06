import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

// T243 [提醒-fix·F-B] — unit test for the create modal's "以本人身份创建提醒文本"
// toggle (deliver_as_creator) plus the §1a remindee multi-select + skip-overlap
// toggle. Data hooks are mocked so the test asserts the toggle renders
// (default ON), the create payload carries the field, and the remindee fans out
// one create per selected agent through the searchable dropdown.

const mutateAsync = vi.fn().mockResolvedValue({});

const updateMutateAsync = vi.fn().mockResolvedValue({});

vi.mock('@/api/reminders', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/reminders')>();
  return {
    ...actual,
    useCreateReminder: () => ({ mutateAsync, isPending: false }),
    useUpdateReminder: () => ({ mutateAsync: updateMutateAsync, isPending: false }),
  };
});
vi.mock('@/api/agents', () => ({
  useAgents: () => ({
    data: [
      { id: 'agent-1', name: 'Dev One' },
      { id: 'agent-2', name: 'Dev Two' },
    ],
    isLoading: false,
  }),
}));

import { ReminderCreateModal, reminderToPrefill } from './ReminderCreateModal';
import type { Reminder } from '@/api/reminders';

function renderModal() {
  return render(<ReminderCreateModal onClose={vi.fn()} />);
}

// Open the remindee dropdown (if not already open — it stays open across
// multi-select picks) and click the option whose row carries data-value.
function pickRemindee(id: string) {
  if (!screen.queryByTestId('reminder-remindee-search')) {
    fireEvent.click(screen.getByTestId('reminder-remindee-trigger'));
  }
  const opt = screen
    .getAllByTestId('reminder-remindee-option')
    .find((el) => el.getAttribute('data-value') === id);
  if (!opt) throw new Error(`no remindee option ${id}`);
  fireEvent.click(opt);
}

describe('ReminderCreateModal — remindee multi-select + toggles', () => {
  afterEach(() => {
    cleanup();
    mutateAsync.mockClear();
  });

  it('renders deliver_as_creator + skip-overlap as toggles (no checkbox), default ON', () => {
    renderModal();
    const deliver = screen.getByTestId('reminder-deliver-as-creator');
    expect(deliver.getAttribute('role')).toBe('switch');
    expect(deliver.getAttribute('aria-checked')).toBe('true');
    const skip = screen.getByTestId('reminder-skip-overlap');
    expect(skip.getAttribute('role')).toBe('switch');
    expect(skip.getAttribute('aria-checked')).toBe('true');
    // No bare checkbox inputs anywhere in the modal (§1a).
    expect(document.querySelector('input[type="checkbox"]')).toBeNull();
  });

  it('submits deliver_as_creator=true by default for the picked remindee', async () => {
    renderModal();
    pickRemindee('agent-1');
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    await waitFor(() =>
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ remindee_agent_id: 'agent-1', deliver_as_creator: true }),
      ),
    );
  });

  it('toggling off submits deliver_as_creator=false', async () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-deliver-as-creator'));
    expect(screen.getByTestId('reminder-deliver-as-creator').getAttribute('aria-checked')).toBe('false');
    pickRemindee('agent-1');
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    await waitFor(() =>
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ deliver_as_creator: false }),
      ),
    );
  });

  it('fans out one create per selected remindee', async () => {
    renderModal();
    pickRemindee('agent-1');
    pickRemindee('agent-2');
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(2));
    const targets = mutateAsync.mock.calls.map((c) => (c[0] as { remindee_agent_id: string }).remindee_agent_id);
    expect(targets).toEqual(['agent-1', 'agent-2']);
  });
});

// New reminder — on_event (event-driven) trigger tab. The third Trigger option
// shows the entity_type / entity_id / event / delay fields, keeps the event
// dropdown scoped to the entity_type's vocabulary, and submits an on_event
// payload (no schedule) matching the backend create_reminder contract.
describe('ReminderCreateModal — on_event trigger', () => {
  afterEach(() => {
    cleanup();
    mutateAsync.mockClear();
  });

  it('offers three trigger tabs (once / cron / on_event)', () => {
    renderModal();
    expect(screen.getByTestId('reminder-kind-once')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-kind-cron')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-kind-on_event')).toBeInTheDocument();
  });

  it('shows on_event fields and hides cron/once fields when selected', () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-kind-on_event'));
    expect(screen.getByTestId('reminder-entity-type')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-entity-id')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-event')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-delay')).toBeInTheDocument();
    // cron/once controls are gone.
    expect(screen.queryByTestId('reminder-cron')).toBeNull();
    expect(screen.queryByTestId('reminder-once-date')).toBeNull();
  });

  it('re-scopes the event dropdown when entity_type changes (no illegal combo)', () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-kind-on_event'));
    const eventSel = screen.getByTestId('reminder-event') as HTMLSelectElement;
    // plan (default) → completed | failed | stopped.
    expect(Array.from(eventSel.options).map((o) => o.value)).toEqual(['completed', 'failed', 'stopped']);
    fireEvent.change(screen.getByTestId('reminder-entity-type'), { target: { value: 'issue' } });
    // issue → closed | reopened; event snaps to the first legal option.
    expect(Array.from(eventSel.options).map((o) => o.value)).toEqual(['closed', 'reopened']);
    expect(eventSel.value).toBe('closed');
  });

  it('submits an on_event payload (on_event + delay, no schedule)', async () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-kind-on_event'));
    pickRemindee('agent-1');
    fireEvent.change(screen.getByTestId('reminder-entity-type'), { target: { value: 'task' } });
    fireEvent.change(screen.getByTestId('reminder-event'), { target: { value: 'blocked' } });
    fireEvent.change(screen.getByTestId('reminder-entity-id'), { target: { value: 'task_123' } });
    fireEvent.change(screen.getByTestId('reminder-delay'), { target: { value: '5m' } });
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    const payload = mutateAsync.mock.calls[0][0] as Record<string, unknown>;
    expect(payload).toMatchObject({
      remindee_agent_id: 'agent-1',
      content: 'ping',
      delay: '5m',
      on_event: { entity_type: 'task', entity_id: 'task_123', event: 'blocked' },
    });
    // on_event mode omits schedule.
    expect(payload.schedule).toBeUndefined();
  });

  it('keeps submit disabled until entity_id is filled', () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-kind-on_event'));
    pickRemindee('agent-1');
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    expect((screen.getByTestId('reminder-submit') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('reminder-entity-id'), { target: { value: 'plan_9' } });
    expect((screen.getByTestId('reminder-submit') as HTMLButtonElement).disabled).toBe(false);
  });
});

// T474 — the prefill prop: open the modal with a remindee pre-selected and, for
// a detected session-limit reset, a one-shot trigger time + content filled in.
describe('ReminderCreateModal — T474 prefill', () => {
  afterEach(() => {
    cleanup();
    mutateAsync.mockClear();
  });

  it('preselects the remindee + once trigger time + content from prefill', () => {
    render(
      <ReminderCreateModal
        onClose={vi.fn()}
        prefill={{
          remindeeIds: ['agent-1'],
          remindeeOptions: [{ id: 'agent-1', name: 'Dev One' }],
          kind: 'once',
          onceDate: '2026-06-25',
          onceTime: '12:10',
          content: 'Session/usage limit reset. Resume your work.',
        }}
      />,
    );
    // Once mode is active → once inputs are present and carry the prefill.
    expect((screen.getByTestId('reminder-once-date') as HTMLInputElement).value).toBe('2026-06-25');
    expect((screen.getByTestId('reminder-once-time') as HTMLInputElement).value).toBe('12:10');
    expect((screen.getByTestId('reminder-content') as HTMLTextAreaElement).value).toContain('limit reset');
    // The remindee chip is rendered (preselected).
    expect(screen.getByTestId('reminder-remindee-chip')).toBeInTheDocument();
    // Submit is enabled (remindee + content + once date are all set).
    expect((screen.getByTestId('reminder-submit') as HTMLButtonElement).disabled).toBe(false);
  });

  it('injects a chip for a prefilled remindee absent from the project agents list', () => {
    render(
      <ReminderCreateModal
        onClose={vi.fn()}
        prefill={{
          remindeeIds: ['agent-outside'],
          remindeeOptions: [{ id: 'agent-outside', name: 'Outsider Bot' }],
        }}
      />,
    );
    // The synthetic option renders a labelled chip even though useAgents() (mocked
    // to agent-1/agent-2 above) doesn't carry agent-outside.
    expect(screen.getByTestId('reminder-remindee-chip')).toHaveTextContent('Outsider Bot');
  });

  it('submits the prefilled once schedule for the prefilled remindee', async () => {
    render(
      <ReminderCreateModal
        onClose={vi.fn()}
        prefill={{
          remindeeIds: ['agent-1'],
          remindeeOptions: [{ id: 'agent-1', name: 'Dev One' }],
          kind: 'once',
          onceDate: '2026-06-25',
          onceTime: '12:10',
          content: 'resume',
        }}
      />,
    );
    fireEvent.click(screen.getByTestId('reminder-submit'));
    await waitFor(() =>
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          remindee_agent_id: 'agent-1',
          content: 'resume',
          schedule: expect.objectContaining({ kind: 'once' }),
        }),
      ),
    );
  });
});

// T477 — reminderToPrefill mapper (clone/edit source) + the modal's edit mode.
const cronReminder: Reminder = {
  id: 'rmd-1',
  organization_id: 'org-1',
  project_id: 'proj-1',
  creator_ref: 'user:pd',
  remindee_agent_id: 'agent-1',
  content: 'run regression',
  status: 'active',
  skip_if_overlap: true,
  deliver_as_creator: true,
  fired_count: 0,
  version: 3,
  schedule: { kind: 'cron', cron_expr: '0 9 * * 1', timezone: 'Asia/Shanghai' },
  next_run_at: null,
  created_at: '2026-06-16T00:00:00Z',
  updated_at: '2026-06-16T00:00:00Z',
};

describe('reminderToPrefill', () => {
  it('maps a cron reminder to a cron prefill (expr + tz + content + remindee)', () => {
    const p = reminderToPrefill(cronReminder, 'Dev One');
    expect(p).toMatchObject({
      remindeeIds: ['agent-1'],
      remindeeOptions: [{ id: 'agent-1', name: 'Dev One' }],
      kind: 'cron',
      cronExpr: '0 9 * * 1',
      tz: 'Asia/Shanghai',
      content: 'run regression',
    });
  });

  it('maps a once reminder to LOCAL date/time that round-trips to the same instant', () => {
    const once: Reminder = { ...cronReminder, schedule: { kind: 'once', once_at: '2026-06-20T05:30:00.000Z' } };
    const p = reminderToPrefill(once);
    expect(p.kind).toBe('once');
    // onceDate/onceTime, parsed as LOCAL wall time, equal the source instant.
    expect(new Date(`${p.onceDate}T${p.onceTime}:00`).toISOString()).toBe('2026-06-20T05:30:00.000Z');
  });
});

describe('ReminderCreateModal — T477 edit mode', () => {
  afterEach(() => {
    cleanup();
    mutateAsync.mockClear();
    updateMutateAsync.mockClear();
  });

  it('edit mode locks the remindee, titles "Edit reminder", and PATCHes via update', () => {
    render(
      <ReminderCreateModal
        editId="rmd-1"
        onClose={vi.fn()}
        prefill={reminderToPrefill(cronReminder, 'Dev One')}
      />,
    );
    expect(screen.getByRole('dialog').getAttribute('aria-label')).toBe('Edit reminder');
    expect(screen.getByTestId('reminder-remindee-trigger')).toBeDisabled();
    expect(screen.queryByTestId('reminder-deliver-as-creator')).toBeNull();
    fireEvent.click(screen.getByTestId('reminder-submit'));
    expect(updateMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'rmd-1',
        action: 'edit',
        content: 'run regression',
        schedule: expect.objectContaining({ kind: 'cron', cron_expr: '0 9 * * 1' }),
      }),
    );
    expect(mutateAsync).not.toHaveBeenCalled();
  });
});
