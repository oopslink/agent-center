import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

// T243 [提醒-fix·F-B] — unit test for the create modal's "以本人身份创建提醒文本"
// toggle (deliver_as_creator) plus the §1a remindee multi-select + skip-overlap
// toggle. Data hooks are mocked so the test asserts the toggle renders
// (default ON), the create payload carries the field, and the remindee fans out
// one create per selected agent through the searchable dropdown.

const mutateAsync = vi.fn().mockResolvedValue({});

vi.mock('@/api/reminders', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/reminders')>();
  return { ...actual, useCreateReminder: () => ({ mutateAsync, isPending: false }) };
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

import { ReminderCreateModal } from './ReminderCreateModal';

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
