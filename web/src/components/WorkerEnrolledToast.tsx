import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppStore } from '@/store/app';

// v2.4-D-F4 (task #44): bottom-right toast that pops when a worker
// finishes enrolling. Fallback for the case where AddWorkerModal is
// already closed (or never was open — e.g. a worker on another machine
// finished enroll on a token previously minted from this console).
//
// Suppressed when AddWorkerModal is open, since the Modal itself shows
// the success state. UI design § 6.
//
// Each toast displays for ~5s then fades out. Multiple back-to-back
// enrollments queue: the newest replaces the visible one (single toast
// slot, not a stack — keeps the corner uncluttered for v0).
interface ToastDetail {
  worker_id?: string;
}

export function WorkerEnrolledToast(): React.ReactElement | null {
  const { t } = useTranslation('members');
  const [visible, setVisible] = useState<{ workerId: string; key: number } | null>(null);

  useEffect(() => {
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent<ToastDetail>).detail || {};
      // Suppress when Modal is open — checked at fire time, not via
      // subscription, so we don't re-render on every store change.
      if (useAppStore.getState().addWorkerModalOpen) return;
      const workerId = detail.worker_id || 'unknown';
      setVisible({ workerId, key: Date.now() });
    };
    window.addEventListener('agent-center:worker-enrolled', handler);
    return () => window.removeEventListener('agent-center:worker-enrolled', handler);
  }, []);

  useEffect(() => {
    if (!visible) return;
    const id = setTimeout(() => setVisible(null), 5_000);
    return () => clearTimeout(id);
  }, [visible]);

  if (!visible) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="worker-enrolled-toast"
      className="fixed bottom-4 right-4 z-50 max-w-sm rounded-lg border border-success/40 bg-success/10 px-4 py-3 shadow-2 motion-safe:animate-fade-in"
    >
      <p className="text-sm font-semibold text-success">{t('workers.enrolledToast.connected')}</p>
      <p className="mt-0.5 font-mono text-xs text-success" data-testid="worker-enrolled-toast-id">
        {visible.workerId}
      </p>
    </div>
  );
}
