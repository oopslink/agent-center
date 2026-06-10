import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useSignout, authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

// AccountPanel renders the self-only account controls (change password + sign
// out). v2.8.1 #8: extracted from the old standalone /me page so the unified
// UserDetail page can show these only when you are viewing your own profile.
export default function AccountPanel(): React.ReactElement {
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
        setPasscodeError('Current password is incorrect');
      } else if (err instanceof ApiError) {
        setPasscodeError(err.message);
      } else {
        setPasscodeError('Change failed, please try again later');
      }
    },
  });

  const validatePasscodeForm = () => {
    if (!/^\d{6}$/.test(newPasscode)) return 'New password must be 6 digits';
    if (newPasscode !== confirmPasscode) return 'Passcodes do not match';
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
        <h4 className="text-sm font-semibold text-text-primary mb-3">Change password</h4>
        {passcodeSuccess && (
          <div role="status" className="mb-3 rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
            Password changed successfully
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
              Current password
            </label>
            <input
              id="current_passcode"
              type="password"
              value={currentPasscode}
              maxLength={128}
              onChange={(e) => setCurrentPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="Current password"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="new_passcode" className="block text-sm text-text-primary">
              New password
            </label>
            <input
              id="new_passcode"
              type="password"
              value={newPasscode}
              maxLength={128}
              onChange={(e) => setNewPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="New password"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="confirm_new_passcode" className="block text-sm text-text-primary">
              Confirm new password
            </label>
            <input
              id="confirm_new_passcode"
              type="password"
              value={confirmPasscode}
              maxLength={128}
              onChange={(e) => setConfirmPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="Confirm new password"
            />
          </div>
          <button
            type="submit"
            disabled={changePasscode.isPending || !currentPasscode || !newPasscode || !confirmPasscode}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {changePasscode.isPending ? 'Changing…' : 'Change password'}
          </button>
        </form>
      </div>

      {/* Sign out */}
      <div className="border-t border-border-base pt-4">
        <h4 className="text-sm font-semibold text-text-primary mb-2">Sign out</h4>
        <button
          type="button"
          onClick={() => signout.mutate()}
          disabled={signout.isPending}
          className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
        >
          {signout.isPending ? 'Signing out…' : 'Sign out'}
        </button>
      </div>
    </div>
  );
}
