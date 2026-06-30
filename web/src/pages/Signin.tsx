import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

export default function Signin(): React.ReactElement {
  const { t } = useTranslation('common');
  const navigate = useNavigate();
  const [displayName, setDisplayName] = useState('');
  const [passcode, setPasscode] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!displayName.trim() || !passcode) return;
    setSubmitting(true);
    setError('');
    try {
      await authApi.signin({ display_name: displayName.trim(), passcode });
      navigate('/', { replace: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError(t('signin.errors.invalidCredentials'));
      } else {
        setError(t('signin.errors.generic'));
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-base px-4">
      <div className="w-full max-w-sm">
        <div className="bg-bg-elevated border border-border rounded-xl p-8 shadow-[var(--shadow-3)]">
          <h1 className="text-2xl font-bold text-text-primary mb-1">{t('signin.title')}</h1>
          <p className="text-sm text-text-muted mb-6">{t('signin.subtitle')}</p>

          {error && (
            <div role="alert" className="mb-4 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="display_name" className="block text-sm font-medium text-text-primary">
                {t('signin.displayNameLabel')}
              </label>
              <input
                id="display_name"
                type="text"
                value={displayName}
                autoComplete="username"
                onChange={(e) => setDisplayName(e.target.value)}
                className="w-full rounded-md border border-border px-3 py-2.5 md:py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted"
                placeholder={t('signin.displayNamePlaceholder')}
              />
            </div>

            <div className="space-y-1">
              <label htmlFor="passcode" className="block text-sm font-medium text-text-primary">
                {t('signin.passwordLabel')}
              </label>
              <input
                id="passcode"
                type="password"
                value={passcode}
                autoComplete="current-password"
                maxLength={128}
                onChange={(e) => setPasscode(e.target.value)}
                className="w-full rounded-md border border-border px-3 py-2.5 md:py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted"
                placeholder={t('signin.passwordPlaceholder')}
              />
            </div>

            <button
              type="submit"
              disabled={submitting || !displayName.trim() || !passcode}
              className="w-full rounded-md bg-brand px-4 py-2.5 md:py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {submitting ? t('signin.submitting') : t('signin.submit')}
            </button>
          </form>

          <p className="mt-4 text-center text-sm text-text-muted">
            {t('signin.noAccount')}{' '}
            <Link to="/signup" className="text-accent hover:underline">
              {t('signin.signupLink')}
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
