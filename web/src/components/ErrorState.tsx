import type React from 'react';
import { useTranslation } from 'react-i18next';

// ErrorState — the #218 friendly-error surface for query/fetch failures.
// Institutional rule #218 ("raw error hidden→[Details]"): NEVER render a raw
// API error string (e.g. "[404 not_found] no such API route") as the primary
// user-facing text. Instead show a friendly headline + tuck the raw error
// behind a keyboard-accessible [Details] <details>/<summary> expander for
// debugging.
//
// Tokens only (both-mode AA): the headline uses `text-danger` (the sanctioned
// danger token, AA in light + dark — never a raw `text-red-*` which the
// a11y guardrail forbids) and the expander body uses `text-text-secondary`
// (AA both modes). No alpha-tint surfaces.
export interface ErrorStateProps {
  /** Friendly, user-facing headline, e.g. "Couldn't load plans." */
  message: string;
  /** The underlying error — its raw text is shown only inside [Details]. */
  error: unknown;
  /** Test hook for the friendly headline. */
  testId?: string;
}

function rawErrorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return String(error);
}

export function ErrorState({ message, error, testId }: ErrorStateProps): React.ReactElement {
  const { t } = useTranslation('common');
  const raw = rawErrorText(error);
  return (
    <div role="alert" data-testid={testId ?? 'error-state'}>
      <p className="text-sm font-medium text-danger">{message}</p>
      <details className="mt-1">
        <summary className="cursor-pointer text-xs text-text-secondary hover:text-text-primary">
          {t('errorState.details')}
        </summary>
        <pre
          className="mt-1 max-w-full overflow-x-auto whitespace-pre-wrap rounded border border-border-base bg-bg-subtle p-2 font-mono text-xs text-text-secondary"
          data-testid={`${testId ?? 'error-state'}-raw`}
        >
          {raw}
        </pre>
      </details>
    </div>
  );
}
