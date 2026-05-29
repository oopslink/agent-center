import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useMe, useSignout, authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

export default function Me(): React.ReactElement {
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
        setPasscodeError('当前密码错误');
      } else if (err instanceof ApiError) {
        setPasscodeError(err.message);
      } else {
        setPasscodeError('修改失败，请稍后重试');
      }
    },
  });

  const validatePasscodeForm = () => {
    if (!/^\d{6}$/.test(newPasscode)) return '新密码必须为 6 位数字';
    if (newPasscode !== confirmPasscode) return '两次输入的密码不一致';
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
      <h2 className="text-xl font-semibold text-text-primary">账户设置</h2>

      {/* Identity Info */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4 space-y-2">
        <h3 className="text-sm font-semibold text-text-primary">账户信息</h3>
        {me.isLoading && <p className="text-sm text-text-muted">加载中…</p>}
        {me.data && (
          <dl className="space-y-1">
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">显示名称</dt>
              <dd className="text-text-primary font-medium">{me.data.display_name}</dd>
            </div>
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">账户 ID</dt>
              <dd className="text-text-secondary font-mono text-xs">{me.data.identity_id}</dd>
            </div>
            <div className="flex gap-2 text-sm">
              <dt className="text-text-muted w-24 flex-shrink-0">类型</dt>
              <dd className="text-text-secondary">{me.data.kind}</dd>
            </div>
          </dl>
        )}
      </div>

      {/* Change Passcode */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4">
        <h3 className="text-sm font-semibold text-text-primary mb-3">修改密码</h3>
        {passcodeSuccess && (
          <div role="status" className="mb-3 rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
            密码修改成功
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
              当前密码
            </label>
            <input
              id="current_passcode"
              type="password"
              value={currentPasscode}
              maxLength={6}
              onChange={(e) => setCurrentPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="new_passcode" className="block text-sm text-text-primary">
              新密码（6 位数字）
            </label>
            <input
              id="new_passcode"
              type="password"
              value={newPasscode}
              maxLength={6}
              onChange={(e) => setNewPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="confirm_new_passcode" className="block text-sm text-text-primary">
              确认新密码
            </label>
            <input
              id="confirm_new_passcode"
              type="password"
              value={confirmPasscode}
              maxLength={6}
              onChange={(e) => setConfirmPasscode(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="••••••"
            />
          </div>
          <button
            type="submit"
            disabled={changePasscode.isPending || !currentPasscode || !newPasscode || !confirmPasscode}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {changePasscode.isPending ? '修改中…' : '修改密码'}
          </button>
        </form>
      </div>

      {/* Sign out */}
      <div className="bg-bg-elevated border border-border rounded-lg p-4">
        <h3 className="text-sm font-semibold text-text-primary mb-2">退出登录</h3>
        <button
          type="button"
          onClick={() => signout.mutate()}
          disabled={signout.isPending}
          className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
        >
          {signout.isPending ? '退出中…' : '退出登录'}
        </button>
      </div>
    </section>
  );
}
