import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useSignout, authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { validatePasscodeStrength, PASSCODE_RULE_HINT } from '@/lib/passcode';

// AccountPanel renders the self-only account controls (change password + sign
// out). v2.8.1 #8: extracted from the old standalone /me page so the unified
// UserDetail page can show these only when you are viewing your own profile.
export default function AccountPanel(): React.ReactElement {
  const { t } = useTranslation('common');
  const signout = useSignout();
  const qc = useQueryClient();

  const [currentPasscode, setCurrentPasscode] = useState('');
  const [newPasscode, setNewPasscode] = useState('');
  const [confirmPasscode, setConfirmPasscode] = useState('');
  const [passcodeError, setPasscodeError] = useState('');
  const [newPasscodeError, setNewPasscodeError] = useState('');
  const [newPasscodeTouched, setNewPasscodeTouched] = useState(false);
  const [passcodeSuccess, setPasscodeSuccess] = useState(false);

  // #290 run-real seam: surface the distinct passcode-strength message inline
  // under the new-password field on blur (and on change once touched), so the
  // user sees WHICH rule failed as they type — not only the form-level error
  // raised on submit. The confirm-match check stays on submit (below).
  const handleNewPasscodeChange = (v: string) => {
    setNewPasscode(v);
    if (newPasscodeTouched) {
      setNewPasscodeError(validatePasscodeStrength(v));
    }
  };

  const handleNewPasscodeBlur = () => {
    setNewPasscodeTouched(true);
    setNewPasscodeError(validatePasscodeStrength(newPasscode));
  };

  const changePasscode = useMutation({
    mutationFn: () =>
      authApi.changePasscode({ current_passcode: currentPasscode, new_passcode: newPasscode }),
    onSuccess: () => {
      setCurrentPasscode('');
      setNewPasscode('');
      setConfirmPasscode('');
      setPasscodeError('');
      setNewPasscodeError('');
      setNewPasscodeTouched(false);
      setPasscodeSuccess(true);
      qc.invalidateQueries({ queryKey: ['me'] });
      setTimeout(() => setPasscodeSuccess(false), 3000);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 401) {
        setPasscodeError(t('accountPanel.errorCurrentPasswordIncorrect'));
      } else if (err instanceof ApiError) {
        setPasscodeError(err.message);
      } else {
        setPasscodeError(t('accountPanel.errorChangeFailed'));
      }
    },
  });

  const validatePasscodeForm = () => {
    const strengthErr = validatePasscodeStrength(newPasscode);
    if (strengthErr) return strengthErr;
    if (newPasscode !== confirmPasscode) return t('accountPanel.errorPasscodesDoNotMatch');
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
    <div className="space-y-6" data-testid="account-panel">
      {/* Change Passcode */}
      <div>
        <h4 className="text-sm font-semibold text-text-primary mb-3">{t('accountPanel.changePasswordHeading')}</h4>
        {passcodeSuccess && (
          <div role="status" className="mb-3 rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
            {t('accountPanel.passwordChangedSuccess')}
          </div>
        )}
        {passcodeError && (
          <div role="alert" className="mb-3 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
            {passcodeError}
          </div>
        )}
        <form onSubmit={handleChangePasscode} noValidate className="space-y-3 max-w-md">
          <div className="space-y-1">
            <label htmlFor="current_passcode" className="block text-sm text-text-primary">
              {t('accountPanel.currentPasswordLabel')}
            </label>
            <input
              id="current_passcode"
              type="password"
              value={currentPasscode}
              maxLength={128}
              onChange={(e) => setCurrentPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder={t('accountPanel.currentPasswordPlaceholder')}
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="new_passcode" className="block text-sm text-text-primary">
              {t('accountPanel.newPasswordLabel')}
            </label>
            <input
              id="new_passcode"
              type="password"
              value={newPasscode}
              maxLength={128}
              onChange={(e) => handleNewPasscodeChange(e.target.value)}
              onBlur={handleNewPasscodeBlur}
              className={`w-full rounded border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary ${
                newPasscodeError ? 'border-danger' : 'border-border'
              }`}
              placeholder={t('accountPanel.newPasswordPlaceholder')}
              aria-describedby={newPasscodeError ? 'new_passcode-err' : undefined}
              aria-invalid={!!newPasscodeError}
            />
            {newPasscodeError && (
              <p id="new_passcode-err" role="alert" className="text-xs text-danger">
                {newPasscodeError}
              </p>
            )}
            <p className="text-xs text-text-secondary">{PASSCODE_RULE_HINT}</p>
          </div>
          <div className="space-y-1">
            <label htmlFor="confirm_new_passcode" className="block text-sm text-text-primary">
              {t('accountPanel.confirmNewPasswordLabel')}
            </label>
            <input
              id="confirm_new_passcode"
              type="password"
              value={confirmPasscode}
              maxLength={128}
              onChange={(e) => setConfirmPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder={t('accountPanel.confirmNewPasswordPlaceholder')}
            />
          </div>
          <button
            type="submit"
            disabled={changePasscode.isPending || !currentPasscode || !newPasscode || !confirmPasscode}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {changePasscode.isPending ? t('accountPanel.changingButton') : t('accountPanel.changePasswordButton')}
          </button>
        </form>
      </div>

      {/* Sign out */}
      <div className="border-t border-border-base pt-4">
        <h4 className="text-sm font-semibold text-text-primary mb-2">{t('accountPanel.signOutHeading')}</h4>
        <button
          type="button"
          onClick={() => signout.mutate()}
          disabled={signout.isPending}
          className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
        >
          {signout.isPending ? t('accountPanel.signingOutButton') : t('accountPanel.signOutButton')}
        </button>
      </div>
    </div>
  );
}
