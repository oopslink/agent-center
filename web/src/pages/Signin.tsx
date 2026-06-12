import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

export default function Signin(): React.ReactElement {
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
        setError('Incorrect display name or password');
      } else {
        setError('Sign-in failed, please try again later');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-base px-4">
      <div className="w-full max-w-sm">
        <div className="bg-bg-elevated border border-border rounded-xl p-8 shadow-[var(--shadow-3)]">
          <h1 className="text-2xl font-bold text-text-primary mb-1">Sign in</h1>
          <p className="text-sm text-text-muted mb-6">Sign in with your Agent Center account</p>

          {error && (
            <div role="alert" className="mb-4 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="display_name" className="block text-sm font-medium text-text-primary">
                Display name
              </label>
              <input
                id="display_name"
                type="text"
                value={displayName}
                autoComplete="username"
                onChange={(e) => setDisplayName(e.target.value)}
                className="w-full rounded-md border border-border px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted"
                placeholder="Your name"
              />
            </div>

            <div className="space-y-1">
              <label htmlFor="passcode" className="block text-sm font-medium text-text-primary">
                Password
              </label>
              <input
                id="passcode"
                type="password"
                value={passcode}
                autoComplete="current-password"
                maxLength={128}
                onChange={(e) => setPasscode(e.target.value)}
                className="w-full rounded-md border border-border px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted"
                placeholder="Your password"
              />
            </div>

            <button
              type="submit"
              disabled={submitting || !displayName.trim() || !passcode}
              className="w-full rounded-md bg-brand px-4 py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {submitting ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <p className="mt-4 text-center text-sm text-text-muted">
            Don't have an account?{' '}
            <Link to="/signup" className="text-accent hover:underline">
              Sign up
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
