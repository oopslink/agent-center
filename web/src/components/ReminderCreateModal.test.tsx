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
