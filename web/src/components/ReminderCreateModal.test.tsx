import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

// T243 [提醒-fix·F-B] — unit test for the create modal's "以本人身份创建提醒文本"
// toggle (deliver_as_creator). Data hooks are mocked so the test asserts the
// toggle renders (default ON) and that the create payload carries the field.

const mutate = vi.fn();

vi.mock('@/api/reminders', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/reminders')>();
  return { ...actual, useCreateReminder: () => ({ mutate, isPending: false }) };
});
vi.mock('@/api/agents', () => ({
  useAgents: () => ({ data: [{ id: 'agent-1', name: 'Dev One' }], isLoading: false }),
}));

import { ReminderCreateModal } from './ReminderCreateModal';

function renderModal() {
  return render(<ReminderCreateModal onClose={vi.fn()} />);
}

describe('ReminderCreateModal — deliver_as_creator toggle (F-B)', () => {
  afterEach(() => {
    cleanup();
    mutate.mockReset();
  });

  it('renders the toggle, default ON', () => {
    renderModal();
    const toggle = screen.getByTestId('reminder-deliver-as-creator');
    expect(toggle).toBeTruthy();
    expect(toggle.getAttribute('aria-checked')).toBe('true');
  });

  it('submits deliver_as_creator=true by default', () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-remindee-pill'));
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({ deliver_as_creator: true }),
      expect.anything(),
    );
  });

  it('toggling off submits deliver_as_creator=false', () => {
    renderModal();
    fireEvent.click(screen.getByTestId('reminder-deliver-as-creator'));
    expect(screen.getByTestId('reminder-deliver-as-creator').getAttribute('aria-checked')).toBe('false');
    fireEvent.click(screen.getByTestId('reminder-remindee-pill'));
    fireEvent.change(screen.getByTestId('reminder-content'), { target: { value: 'ping' } });
    fireEvent.click(screen.getByTestId('reminder-submit'));
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({ deliver_as_creator: false }),
      expect.anything(),
    );
  });
});
