import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { WakeGuardrail } from '@/api/system';

// I7-D3 — unit test for the wake-guardrail params panel. The system API hooks
// are mocked so the test asserts the panel's render + edit/validate/save logic.

const mutate = vi.fn();
let queryState: { data?: WakeGuardrail; isLoading: boolean; isError: boolean } = {
  data: {
    max_depth: 4,
    cycle_window_sec: 300,
    cycle_threshold: 3,
    rate_per_min: 10,
    chain_token_budget: 16,
  },
  isLoading: false,
  isError: false,
};

vi.mock('@/api/system', () => ({
  useWakeGuardrail: () => queryState,
  useUpdateWakeGuardrail: () => ({ mutate, isPending: false, isError: false, error: null }),
}));

import { WakeGuardrailPanel } from './WakeGuardrailPanel';

beforeEach(() => {
  queryState = {
    data: { max_depth: 4, cycle_window_sec: 300, cycle_threshold: 3, rate_per_min: 10, chain_token_budget: 16 },
    isLoading: false,
    isError: false,
  };
});
afterEach(() => {
  cleanup();
  mutate.mockReset();
});

describe('WakeGuardrailPanel', () => {
  it('renders the five thresholds seeded from the effective config', () => {
    render(<WakeGuardrailPanel />);
    expect((screen.getByTestId('wake-guardrail-max_depth') as HTMLInputElement).value).toBe('4');
    expect((screen.getByTestId('wake-guardrail-cycle_window_sec') as HTMLInputElement).value).toBe('300');
    expect((screen.getByTestId('wake-guardrail-cycle_threshold') as HTMLInputElement).value).toBe('3');
    expect((screen.getByTestId('wake-guardrail-rate_per_min') as HTMLInputElement).value).toBe('10');
    expect((screen.getByTestId('wake-guardrail-chain_token_budget') as HTMLInputElement).value).toBe('16');
  });

  it('saves the edited thresholds via PUT', () => {
    render(<WakeGuardrailPanel />);
    fireEvent.change(screen.getByTestId('wake-guardrail-max_depth'), { target: { value: '6' } });
    fireEvent.click(screen.getByTestId('wake-guardrail-save'));
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({ max_depth: 6, cycle_window_sec: 300, chain_token_budget: 16 }),
      expect.anything(),
    );
  });

  it('blocks save on a non-positive threshold', () => {
    render(<WakeGuardrailPanel />);
    fireEvent.change(screen.getByTestId('wake-guardrail-rate_per_min'), { target: { value: '0' } });
    expect(screen.getByTestId('wake-guardrail-invalid')).toBeTruthy();
    expect((screen.getByTestId('wake-guardrail-save') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId('wake-guardrail-save'));
    expect(mutate).not.toHaveBeenCalled();
  });

  it('shows the loading state while fetching', () => {
    queryState = { data: undefined, isLoading: true, isError: false };
    render(<WakeGuardrailPanel />);
    expect(screen.getByTestId('wake-guardrail-loading')).toBeTruthy();
  });
});
