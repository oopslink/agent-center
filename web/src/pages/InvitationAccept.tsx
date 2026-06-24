import type React from 'react';
import { useMutation } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { invitationsApi } from '@/api/invitations';

export default function InvitationAccept(): React.ReactElement {
  const { token } = useParams();
  const accept = useMutation({
    mutationFn: () => invitationsApi.accept(token ?? ''),
  });

  return (
    <main className="flex min-h-screen items-center justify-center bg-bg-subtle px-4 text-text-primary">
      <section className="w-full max-w-md rounded border border-border-base bg-bg-elevated p-6 shadow-[var(--shadow-2)]">
        <h1 className="text-xl font-semibold">Organization Invitation</h1>
        <p className="mt-2 text-sm text-text-secondary">
          Sign in with the invited user account before accepting. Invitations cannot create new users.
        </p>
        {accept.isIdle && (
          <button
            type="button"
            disabled={!token}
            onClick={() => accept.mutate()}
            className="mt-5 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          >
            Accept Invitation
          </button>
        )}
        {accept.isPending && <p className="mt-5 text-sm text-text-muted">Accepting...</p>}
        {accept.isSuccess && (
          <div className="mt-5 rounded border border-success/30 bg-success/10 px-3 py-2 text-sm text-success">
            Invitation accepted. You can now open this organization from the org switcher.
          </div>
        )}
        {accept.isError && (
          <div className="mt-5 rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            {accept.error instanceof ApiError ? accept.error.message : 'Invitation could not be accepted.'}
          </div>
        )}
      </section>
    </main>
  );
}
