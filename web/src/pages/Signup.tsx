import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

function validateDisplayName(v: string): string {
  if (!v.trim()) return '请输入显示名称';
  if (v.length > 40) return '显示名称最多 40 个字符';
  return '';
}

function validatePasscode(v: string): string {
  if (!/^\d{6}$/.test(v)) return '请输入 6 位数字密码';
  return '';
}

function validateConfirm(p: string, c: string): string {
  if (p !== c) return '两次输入的密码不一致';
  return '';
}

function validateOrgName(v: string): string {
  if (!v.trim()) return '请输入组织名称';
  if (v.length > 80) return '组织名称最多 80 个字符';
  return '';
}

function validateSlug(v: string): string {
  if (v.length < 3) return 'Slug 至少 3 个字符';
  if (v.length > 40) return 'Slug 最多 40 个字符';
  if (!/^[a-z0-9-]+$/.test(v)) return 'Slug 只能包含 [a-z0-9-]';
  if (/^-|-$/.test(v)) return 'Slug 不能以连字符开头或结尾';
  if (/--/.test(v)) return 'Slug 不能包含连续连字符';
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
}

function Field({ id, label, type = 'text', value, error, placeholder, maxLength, onChange }: FieldProps) {
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
        className={`w-full rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary placeholder:text-text-muted ${
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
  const [passcode, setPasscode] = useState('');
  const [confirmPasscode, setConfirmPasscode] = useState('');
  const [orgName, setOrgName] = useState('');
  const [orgSlug, setOrgSlug] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const autoSlug = (name: string) =>
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 40);

  const handleOrgNameChange = (v: string) => {
    setOrgName(v);
    if (!orgSlug || orgSlug === autoSlug(orgName)) {
      setOrgSlug(autoSlug(v));
    }
  };

  const validate = () => {
    const errs: Record<string, string> = {};
    const e1 = validateDisplayName(displayName);
    if (e1) errs.displayName = e1;
    const e2 = validatePasscode(passcode);
    if (e2) errs.passcode = e2;
    const e3 = validateConfirm(passcode, confirmPasscode);
    if (e3) errs.confirmPasscode = e3;
    const e4 = validateOrgName(orgName);
    if (e4) errs.orgName = e4;
    const e5 = validateSlug(orgSlug);
    if (e5) errs.orgSlug = e5;
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
        passcode,
        organization_name: orgName.trim(),
        organization_slug: orgSlug,
      });
      navigate('/', { replace: true });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'display_name_taken') {
          setErrors((prev) => ({ ...prev, displayName: '该显示名称已被使用' }));
        } else if (err.code === 'slug_taken') {
          setErrors((prev) => ({ ...prev, orgSlug: '该 Slug 已被使用' }));
        } else {
          setServerError(err.message);
        }
      } else {
        setServerError('注册失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-base px-4">
      <div className="w-full max-w-md">
        <div className="bg-bg-elevated border border-border rounded-xl p-8 shadow-[var(--shadow-3)]">
          <h1 className="text-2xl font-bold text-text-primary mb-1">创建账户</h1>
          <p className="text-sm text-text-muted mb-6">设置你的 Agent Center 账户和第一个组织</p>

          {serverError && (
            <div role="alert" className="mb-4 rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
              {serverError}
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <Field
              id="display_name"
              label="显示名称"
              value={displayName}
              error={errors.displayName ?? ''}
              placeholder="你的名字"
              maxLength={40}
              onChange={setDisplayName}
            />
            <Field
              id="passcode"
              label="6 位数字密码"
              type="password"
              value={passcode}
              error={errors.passcode ?? ''}
              placeholder="••••••"
              maxLength={6}
              onChange={setPasscode}
            />
            <Field
              id="confirm_passcode"
              label="确认密码"
              type="password"
              value={confirmPasscode}
              error={errors.confirmPasscode ?? ''}
              placeholder="••••••"
              maxLength={6}
              onChange={setConfirmPasscode}
            />

            <hr className="border-border" />

            <Field
              id="org_name"
              label="组织名称"
              value={orgName}
              error={errors.orgName ?? ''}
              placeholder="My Organization"
              maxLength={80}
              onChange={handleOrgNameChange}
            />
            <Field
              id="org_slug"
              label="组织 Slug（URL 路径）"
              value={orgSlug}
              error={errors.orgSlug ?? ''}
              placeholder="my-org"
              maxLength={40}
              onChange={setOrgSlug}
            />

            <button
              type="submit"
              disabled={submitting || !isValid()}
              className="w-full rounded-md bg-brand px-4 py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {submitting ? '注册中…' : '创建账户'}
            </button>
          </form>

          <p className="mt-4 text-center text-sm text-text-muted">
            已有账户？{' '}
            <Link to="/signin" className="text-accent hover:underline">
              登录
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
