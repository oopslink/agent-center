import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { validatePasscodeStrength, PASSCODE_RULE_HINT } from '@/lib/passcode';

function validateDisplayName(v: string): string {
  if (!v.trim()) return 'Please enter a display name';
  if (v.length > 40) return 'Display name must be at most 40 characters';
  return '';
}

function validateEmail(v: string): string {
  if (!v.trim()) return 'Please enter an email';
  // Lightweight format check (backend stores without verifying).
  if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v.trim())) return 'Please enter a valid email';
  return '';
}

function validatePasscode(v: string): string {
  return validatePasscodeStrength(v);
}

function validateConfirm(p: string, c: string): string {
  if (p !== c) return 'Passcodes do not match';
  return '';
}

function validateOrgName(v: string): string {
  if (!v.trim()) return 'Please enter an organization name';
  if (v.length > 80) return 'Organization name must be at most 80 characters';
  return '';
}

interface FieldProps {
  id: string;
  label: string;
  type?: string;
  value: string;
  error: string;
  placeholder?: string;
  maxLength?: number;
  onChange: (v: string) => void;
  onBlur?: () => void;
}

function Field({ id, label, type = 'text', value, error, placeholder, maxLength, onChange, onBlur }: FieldProps) {
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="block text-sm font-medium text-text-primary">
        {label}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        maxLength={maxLength}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        className={`w-full rounded-md border px-3 py-2.5 md:py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted ${
          error ? 'border-danger' : 'border-border'
        }`}
        aria-describedby={error ? `${id}-err` : undefined}
        aria-invalid={!!error}
      />
      {error && (
        <p id={`${id}-err`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  );
}

export default function Signup(): React.ReactElement {
  const navigate = useNavigate();
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [passcode, setPasscode] = useState('');
  const [confirmPasscode, setConfirmPasscode] = useState('');
  const [orgName, setOrgName] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [passcodeTouched, setPasscodeTouched] = useState(false);
  const [serverError, setServerError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // #290 run-real seam: the submit button is disabled while the form is
  // invalid, so handleSubmit (which populates `errors`) never fires for an
  // invalid passcode and the user never saw WHICH rule failed. Validate the
  // passcode on blur, and on subsequent change once it has been touched, so
  // the existing inline `errors.passcode` element renders the distinct
  // strength message as the user types/leaves the field.
  const handlePasscodeChange = (v: string) => {
    setPasscode(v);
    if (passcodeTouched) {
      setErrors((prev) => ({ ...prev, passcode: validatePasscode(v) }));
    }
  };

  const handlePasscodeBlur = () => {
    setPasscodeTouched(true);
    setErrors((prev) => ({ ...prev, passcode: validatePasscode(passcode) }));
  };

  const validate = () => {
    const errs: Record<string, string> = {};
    const e1 = validateDisplayName(displayName);
    if (e1) errs.displayName = e1;
    const eEmail = validateEmail(email);
    if (eEmail) errs.email = eEmail;
    const e2 = validatePasscode(passcode);
    if (e2) errs.passcode = e2;
    const e3 = validateConfirm(passcode, confirmPasscode);
    if (e3) errs.confirmPasscode = e3;
    const e4 = validateOrgName(orgName);
    if (e4) errs.orgName = e4;
    return errs;
  };

  const isValid = () => {
    const errs = validate();
    return Object.keys(errs).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const errs = validate();
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    setSubmitting(true);
    setServerError('');
    try {
      await authApi.signup({
        display_name: displayName.trim(),
        email: email.trim(),
        passcode,
        organization_name: orgName.trim(),
      });
      navigate('/', { replace: true });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'display_name_taken') {
          setErrors((prev) => ({ ...prev, displayName: 'That display name is already taken' }));
        } else if (err.code === 'email_taken' || err.code === 'already_exists') {
          setErrors((prev) => ({ ...prev, email: 'That email is already in use' }));
        } else {
          setServerError(err.message);
        }
      } else {
        setServerError('Sign-up failed, please try again later');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-base px-4">
      <div className="w-full max-w-md">
        <div className="bg-bg-elevated border border-border rounded-xl p-8 shadow-[var(--shadow-3)]">
          <h1 className="text-2xl font-bold text-text-primary mb-1">Create account</h1>
          <p className="text-sm text-text-muted mb-6">Set up your Agent Center account and first organization</p>

          {serverError && (
            <div role="alert" className="mb-4 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
              {serverError}
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <Field
              id="display_name"
              label="Display name"
              value={displayName}
              error={errors.displayName ?? ''}
              placeholder="Your name"
              maxLength={40}
              onChange={setDisplayName}
            />
            <Field
              id="email"
              label="Email"
              type="email"
              value={email}
              error={errors.email ?? ''}
              placeholder="you@example.com"
              maxLength={200}
              onChange={setEmail}
            />
            <div className="space-y-1">
              <Field
                id="passcode"
                label="Passcode"
                type="password"
                value={passcode}
                error={errors.passcode ?? ''}
                placeholder="Your passcode"
                maxLength={128}
                onChange={handlePasscodeChange}
                onBlur={handlePasscodeBlur}
              />
              <p className="text-xs text-text-secondary">{PASSCODE_RULE_HINT}</p>
            </div>
            <Field
              id="confirm_passcode"
              label="Confirm passcode"
              type="password"
              value={confirmPasscode}
              error={errors.confirmPasscode ?? ''}
              placeholder="Confirm passcode"
              maxLength={128}
              onChange={setConfirmPasscode}
            />

            <hr className="border-border" />

            <Field
              id="org_name"
              label="Organization name"
              value={orgName}
              error={errors.orgName ?? ''}
              placeholder="My Organization"
              maxLength={80}
              onChange={setOrgName}
            />

            <button
              type="submit"
              disabled={submitting || !isValid()}
              className="w-full rounded-md bg-brand px-4 py-2.5 md:py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {submitting ? 'Signing up…' : 'Create account'}
            </button>
          </form>

          <p className="mt-4 text-center text-sm text-text-muted">
            Already have an account?{' '}
            <Link to="/signin" className="text-accent hover:underline">
              Sign in
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
