import type React from 'react';
import { useState } from 'react';
import { useRevokeSecret, useSecrets } from '@/api/secrets';
import { formatLocalTime } from '@/utils/time';
import type { Secret } from '@/api/types';
import { SecretCreateModal } from '@/components/SecretCreateModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

// Secrets page (/secrets). List + create + revoke.
//
// Strict no-plaintext-echo per ADR-0026 § 5:
//   - render columns: name / kind / state / created_at / created_by /
//     revoked_at (when revoked). No value, no reveal.
//   - revoke is the only mutation on existing rows; rotation = revoke
//     + create new.
export default function Secrets(): React.ReactElement {
  const [createOpen, setCreateOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<Secret | null>(null);
  const secrets = useSecrets();
  const revoke = useRevokeSecret();

  const confirmRevoke = () => {
    if (!revokeTarget) return;
    revoke.mutate(revokeTarget.id, { onSuccess: () => setRevokeTarget(null) });
  };

  return (
    <section className="space-y-4" data-testid="page-Secrets">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Secrets</h1>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          data-testid="secrets-new-button"
        >
          New secret
        </button>
      </header>

      <p className="text-xs text-text-muted" data-testid="secrets-disclaimer">
        Values are stored encrypted and never displayed after creation. To
        rotate a secret, revoke and create a new one.
      </p>

      {secrets.isLoading && (
        <div className="space-y-2" data-testid="secrets-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {secrets.isError && (
        <p className="text-sm text-danger" data-testid="secrets-error">
          {(secrets.error as Error).message}
        </p>
      )}
      {secrets.isSuccess && secrets.data.length === 0 && (
        <EmptyState
          testId="secrets-empty"
          title="No secrets yet"
          body="Secrets store API keys + tokens that agents reference at runtime. Values are encrypted at rest and never displayed after creation."
          action={{ label: 'New secret', onClick: () => setCreateOpen(true) }}
        />
      )}
      {secrets.isSuccess && secrets.data.length > 0 && (
        <>
        {/* Mobile card view */}
        <ul className="space-y-2 md:hidden">
          {secrets.data.map((s) => (
            <li key={s.id} className="rounded-lg border border-border-base bg-bg-elevated p-3" data-testid="secret-card-mobile" data-secret-id={s.id}>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-text-primary">{s.name}</span>
                {s.state === 'active' && (
                  <button
                    type="button"
                    onClick={() => setRevokeTarget(s)}
                    disabled={revoke.isPending}
                    className="rounded px-3 py-2 text-xs text-danger hover:bg-bg-subtle disabled:opacity-50"
                    data-testid="secret-revoke-button-mobile"
                  >
                    Revoke
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
        <table
          className="hidden w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary md:table"
          data-testid="secrets-table"
        >
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
              <th className="w-1/4 border-b border-border-base px-3 py-2">Name</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Kind</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">State</th>
              <th className="w-1/4 border-b border-border-base px-3 py-2">Created</th>
              <th className="border-b border-border-base px-3 py-2 text-right" />
            </tr>
          </thead>
          <tbody>
            {secrets.data.map((s) => (
              <tr
                key={s.id}
                className="text-sm"
                data-testid="secret-row"
                data-secret-id={s.id}
                data-secret-state={s.state}
              >
                <td className="border-b border-border-base px-3 py-2 font-medium">{s.name}</td>
                <td className="border-b border-border-base px-3 py-2 font-mono text-xs">
                  {s.kind}
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <span
                    className={[
                      'rounded px-2 py-0.5 text-xs uppercase',
                      s.state === 'active'
                        ? 'bg-success/20 text-success'
                        : 'bg-bg-subtle text-text-secondary',
                    ].join(' ')}
                  >
                    {s.state}
                  </span>
                </td>
                <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted" title={s.created_at}>
                  {formatLocalTime(s.created_at)}
                </td>
                <td className="border-b border-border-base px-3 py-2 text-right">
                  {s.state === 'active' && (
                    <button
                      type="button"
                      onClick={() => setRevokeTarget(s)}
                      disabled={revoke.isPending}
                      className="rounded px-3 py-1 text-xs text-danger hover:bg-bg-subtle disabled:opacity-50"
                      data-testid="secret-revoke-button"
                    >
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </>
      )}

      <SecretCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
      />

      <ConfirmModal
        open={revokeTarget !== null}
        title="Revoke secret?"
        message={
          revokeTarget
            ? `Revoke secret "${revokeTarget.name}"? This cannot be undone.`
            : undefined
        }
        confirmLabel="Revoke"
        danger
        busy={revoke.isPending}
        onConfirm={confirmRevoke}
        onCancel={() => setRevokeTarget(null)}
      />
    </section>
  );
}
