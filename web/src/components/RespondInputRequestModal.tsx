import type React from 'react';
import { useState } from 'react';
import { useRespondInputRequest } from '@/api/inputRequests';
import type { InputRequest } from '@/api/types';
import { useModalA11y } from './useModalA11y';

interface Props {
  open: boolean;
  ir: InputRequest | null;
  onClose: () => void;
}

// RespondInputRequestModal — textarea + Send. If the IR carries a
// `suggested_response` (currently not surfaced by the backend
// projection but planned per oversight #3), an "Adopt suggestion"
// button pre-fills the textarea.
export function RespondInputRequestModal({
  open,
  ir,
  onClose,
}: Props): React.ReactElement | null {
  const [answer, setAnswer] = useState('');
  const respond = useRespondInputRequest();
  const close = () => {
    setAnswer('');
    onClose();
  };
  const containerRef = useModalA11y({ open, onClose: close });
  if (!open || !ir) return null;

  // Defensive typing: read the optional field through a structural cast
  // rather than extending the InputRequest interface upstream. Today's
  // backend doesn't emit it; the UI is forward-compatible.
  const suggested = (ir as { suggested_response?: string }).suggested_response;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!answer.trim()) return;
    try {
      await respond.mutateAsync({ id: ir.id, answer: answer.trim() });
      close();
    } catch {
      // Error renders below; modal stays open for retry.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ir-respond-title"
      data-testid="ir-respond-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="ir-respond-title" className="text-lg font-semibold">
          Respond to input request
        </h2>
        <p className="mt-1 text-sm text-text-secondary">{ir.question}</p>
        {(ir.options ?? []).length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1" data-testid="ir-options">
            {ir.options!.map((o) => (
              <button
                key={o}
                type="button"
                onClick={() => setAnswer(o)}
                className="rounded-full bg-bg-subtle px-2 py-0.5 text-xs text-text-secondary hover:bg-border-base"
                data-testid="ir-option-chip"
              >
                {o}
              </button>
            ))}
          </div>
        )}
        <form className="mt-3 space-y-3" onSubmit={submit}>
          <textarea
            rows={3}
            value={answer}
            onChange={(e) => setAnswer(e.target.value)}
            placeholder="your response"
            autoFocus
            className="w-full resize-none rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
            data-testid="ir-answer-textarea"
          />
          {suggested && (
            <button
              type="button"
              onClick={() => setAnswer(suggested)}
              className="text-xs text-accent hover:underline"
              data-testid="ir-adopt-suggestion"
            >
              Adopt supervisor suggestion
            </button>
          )}
          {respond.isError && (
            <p className="text-xs text-danger" data-testid="ir-respond-error">
              {(respond.error as Error).message}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={close}
              className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              data-testid="ir-respond-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!answer.trim() || respond.isPending}
              className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="ir-respond-submit"
            >
              {respond.isPending ? 'Sending…' : 'Send'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
