import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppStore } from '@/store/app';
import { WorkerEnrolledToast } from './WorkerEnrolledToast';

function fireEnrolled(workerId: string): void {
  window.dispatchEvent(
    new CustomEvent('agent-center:worker-enrolled', { detail: { worker_id: workerId } }),
  );
}

describe('WorkerEnrolledToast', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useAppStore.setState({ addWorkerModalOpen: false });
  });
  afterEach(() => {
    vi.useRealTimers();
    cleanup();
  });

  it('renders the worker_id when an enrolled event fires and modal is closed', () => {
    render(<WorkerEnrolledToast />);
    act(() => fireEnrolled('worker-aabb'));
    expect(screen.getByTestId('worker-enrolled-toast')).toBeInTheDocument();
    expect(screen.getByTestId('worker-enrolled-toast-id')).toHaveTextContent('worker-aabb');
  });

  it('hides itself after 5 seconds', () => {
    render(<WorkerEnrolledToast />);
    act(() => fireEnrolled('w1'));
    expect(screen.getByTestId('worker-enrolled-toast')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(5_001);
    });
    expect(screen.queryByTestId('worker-enrolled-toast')).toBeNull();
  });

  it('is suppressed while AddWorkerModal is open', () => {
    useAppStore.setState({ addWorkerModalOpen: true });
    render(<WorkerEnrolledToast />);
    act(() => fireEnrolled('w-skip'));
    expect(screen.queryByTestId('worker-enrolled-toast')).toBeNull();
  });

  it('falls back to "unknown" when payload omits worker_id', () => {
    render(<WorkerEnrolledToast />);
    act(() =>
      window.dispatchEvent(new CustomEvent('agent-center:worker-enrolled', { detail: {} })),
    );
    expect(screen.getByTestId('worker-enrolled-toast-id')).toHaveTextContent('unknown');
  });
});
