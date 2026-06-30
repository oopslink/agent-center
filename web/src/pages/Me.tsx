import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useMe, useSignout, authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { useTranslation } from 'react-i18next';

export default function Me(): React.ReactElement {
  const { t } = useTranslation('members');
  const me = useMe();
  const signout = useSignout();
  const qc = useQueryClient();

  const [currentPasscode, setCurrentPasscode] = useState('');
  const [newPasscode, setNewPasscode] = useState('');
  const [confirmPasscode, setConfirmPasscode] = useState('');
  const [passcodeError, setPasscodeError] = useState('');
  const [passcodeSuccess, setPasscodeSuccess] = useState(false);

  const changePasscode = useMutation({
    mutationFn: () =>
      authApi.changePasscode({ current_passcode: currentPasscode, new_passcode: newPasscode }),
    onSuccess: () => {
      setCurrentPasscode('');
      setNewPasscode('');
      setConfirmPasscode('');
      setPasscodeError('');
      setPasscodeSuccess(true);
      qc.invalidateQueries({ queryKey: ['me'] });
      setTimeout(() => setPasscodeSuccess(false), 3000);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 401) {
        setPasscodeError(t('humans.me.errors.currentIncorrect'));
      } else if (err instanceof ApiError) {
        setPasscodeError(err.message);
      } else {
        setPasscodeError(t('humans.me.errors.changeFailed'));
      }
    },
  });

  const validatePasscodeForm = () => {
    if (!/^\d{6}$/.test(newPasscode)) return t('humans.me.errors.newMustBe6Digits');
    if (newPasscode !== confirmPasscode) return t('humans.me.errors.passcodesMismatch');
    return '';
  };

  const handleChangePasscode = (e: React.FormEvent) => {
    e.preventDefault();
    setPasscodeError('');
    setPasscodeSuccess(false);
    const err = validatePasscodeForm();
    if (err) {
      setPasscodeError(err);
      return;
    }
    changePasscode.mutate();
  };

  return (
    <section className="space-y-6 max-w-md" data-testid="page-Me">
      <h2 className="text-xl font-semibold text-text-primary">{t('humans.me.title')}</h2>

      {/* Identity Info */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4 space-y-2">
        <h3 className="text-sm font-semibold text-text-primary">{t('humans.me.accountInfo')}</h3>
        {me.isLoading && <p className="text-sm text-text-muted">{t('humans.me.loading')}</p>}
        {me.data && (
          <dl className="space-y-1">
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">{t('humans.me.displayName')}</dt>
              <dd className="text-text-primary font-medium">{me.data.display_name}</dd>
            </div>
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">{t('humans.me.accountId')}</dt>
              <dd className="text-text-secondary font-mono text-xs">{me.data.identity_id}</dd>
            </div>
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">{t('humans.me.type')}</dt>
              <dd className="text-text-secondary">{me.data.kind}</dd>
            </div>
          </dl>
        )}
      </div>

      {/* Change Passcode */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4">
        <h3 className="text-sm font-semibold text-text-primary mb-3">{t('humans.me.changePassword')}</h3>
        {passcodeSuccess && (
          <div role="status" className="mb-3 rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
            {t('humans.me.changeSuccess')}
          </div>
        )}
        {passcodeError && (
          <div role="alert" className="mb-3 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
            {passcodeError}
          </div>
        )}
        <form onSubmit={handleChangePasscode} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="current_passcode" className="block text-sm text-text-primary">
              {t('humans.me.currentPassword')}
            </label>
            <input
              id="current_passcode"
              type="password"
              value={currentPasscode}
              maxLength={6}
              onChange={(e) => setCurrentPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-2 md:py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="new_passcode" className="block text-sm text-text-primary">
              {t('humans.me.newPassword')}
            </label>
            <input
              id="new_passcode"
              type="password"
              value={newPasscode}
              maxLength={6}
              onChange={(e) => setNewPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-2 md:py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="confirm_new_passcode" className="block text-sm text-text-primary">
              {t('humans.me.confirmNewPassword')}
            </label>
            <input
              id="confirm_new_passcode"
              type="password"
              value={confirmPasscode}
              maxLength={6}
              onChange={(e) => setConfirmPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-2 md:py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <button
            type="submit"
            disabled={changePasscode.isPending || !currentPasscode || !newPasscode || !confirmPasscode}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {changePasscode.isPending ? t('humans.me.changing') : t('humans.me.changePassword')}
          </button>
        </form>
      </div>

      {/* Sign out */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4">
        <h3 className="text-sm font-semibold text-text-primary mb-2">{t('humans.me.signOut')}</h3>
        <button
          type="button"
          onClick={() => signout.mutate()}
          disabled={signout.isPending}
          className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
        >
          {signout.isPending ? t('humans.me.signingOut') : t('humans.me.signOut')}
        </button>
      </div>
    </section>
  );
}
