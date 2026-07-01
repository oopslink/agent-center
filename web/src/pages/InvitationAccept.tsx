import type React from 'react';
import { useMutation } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { invitationsApi } from '@/api/invitations';

export default function InvitationAccept(): React.ReactElement {
  const { t } = useTranslation('common');
  const { token } = useParams();
  const accept = useMutation({
    mutationFn: () => invitationsApi.accept(token ?? ''),
  });

  return (
    <main className="flex min-h-screen items-center justify-center bg-bg-subtle px-4 text-text-primary">
      <section className="w-full max-w-md rounded border border-border-base bg-bg-elevated p-6 shadow-[var(--shadow-2)]">
        <h1 className="text-xl font-semibold">{t('invitationAccept.title')}</h1>
        <p className="mt-2 text-sm text-text-secondary">
          {t('invitationAccept.description')}
        </p>
        {accept.isIdle && (
          <button
            type="button"
            disabled={!token}
            onClick={() => accept.mutate()}
            className="mt-5 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          >
            {t('invitationAccept.accept')}
          </button>
        )}
        {accept.isPending && <p className="mt-5 text-sm text-text-muted">{t('invitationAccept.accepting')}</p>}
        {accept.isSuccess && (
          <div className="mt-5 rounded border border-success/30 bg-success/10 px-3 py-2 text-sm text-success">
            {t('invitationAccept.success')}
          </div>
        )}
        {accept.isError && (
          <div className="mt-5 rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            {accept.error instanceof ApiError ? accept.error.message : t('invitationAccept.errorFallback')}
          </div>
        )}
      </section>
    </main>
  );
}
