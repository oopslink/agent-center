import React, { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOrgs, orgApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';

export default function OrgSettings(): React.ReactElement {
  const orgs = useOrgs();
  const orgCtx = useOptionalOrgContext();
  const qc = useQueryClient();
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [nameDraft, setNameDraft] = useState('');
  const [nameEditing, setNameEditing] = useState(false);

  // v2.6 multi-org: use the org from URL slug (OrgGuard context), not "first org".
  const org = orgCtx
    ? (orgs.data ?? []).find((o) => o.slug === orgCtx.slug)
    : orgs.data?.[0];

  useEffect(() => {
    if (org) setNameDraft(org.name);
  }, [org]);

  const updateName = useMutation({
    mutationFn: () => orgApi.update(org!.id, { name: nameDraft.trim() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setNameEditing(false);
      setSuccess('组织名称已更新');
      setTimeout(() => setSuccess(''), 3000);
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError('更新失败');
    },
  });

  const deleteOrg = useMutation({
    mutationFn: () => orgApi.delete(org!.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setDeleteConfirm(false);
      setSuccess('组织已删除');
      setTimeout(() => { window.location.href = '/'; }, 1000);
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError('删除失败，请稍后重试');
    },
  });

  return (
    <section className="space-y-6 max-w-md" data-testid="page-OrgSettings">
      <h2 className="text-xl font-semibold text-text-primary">组织设置</h2>

      {success && (
        <div role="status" className="rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
          {success}
        </div>
      )}

      {orgs.isLoading && <p className="text-sm text-text-muted">加载中…</p>}

      {org && (
        <>
          <div className="bg-bg-elevated border border-border rounded-lg p-4 space-y-3">
            <h3 className="text-sm font-semibold text-text-primary">组织信息</h3>
            <div className="space-y-2">
              <div className="space-y-1">
                <label className="text-xs text-text-muted">名称</label>
                {!nameEditing ? (
                  <div className="flex items-center justify-between">
                    <span className="text-text-primary">{org.name}</span>
                    <button
                      type="button"
                      onClick={() => { setNameDraft(org.name); setNameEditing(true); setError(''); }}
                      className="text-xs text-accent hover:underline"
                    >
                      编辑
                    </button>
                  </div>
                ) : (
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={nameDraft}
                      onChange={(e) => setNameDraft(e.target.value)}
                      maxLength={80}
                      className="flex-1 rounded border border-border px-2 py-1 text-sm bg-bg-elevated text-text-primary"
                    />
                    <button
                      type="button"
                      onClick={() => updateName.mutate()}
                      disabled={updateName.isPending || !nameDraft.trim() || nameDraft.trim() === org.name}
                      className="rounded bg-brand px-3 py-1 text-xs text-white hover:bg-brand-hover disabled:opacity-50"
                    >
                      保存
                    </button>
                    <button
                      type="button"
                      onClick={() => { setNameEditing(false); setError(''); }}
                      className="rounded px-3 py-1 text-xs text-text-secondary hover:bg-bg-subtle"
                    >
                      取消
                    </button>
                  </div>
                )}
              </div>
              <div className="space-y-1">
                <label className="text-xs text-text-muted">Slug</label>
                <code className="block text-xs text-text-secondary font-mono">{org.slug}</code>
              </div>
            </div>
            <p className="text-xs text-text-muted pt-1">
              Slug / description 编辑功能为后续 schema follow-up（领域字段未就绪）。
            </p>
          </div>

          {error && (
            <div role="alert" className="rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
              {error}
            </div>
          )}

          <div className="bg-bg-elevated border border-border rounded-lg p-4">
            <h3 className="text-sm font-semibold text-danger mb-2">危险操作</h3>
            {!deleteConfirm ? (
              <button
                type="button"
                onClick={() => setDeleteConfirm(true)}
                className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10"
              >
                删除组织
              </button>
            ) : (
              <div className="space-y-2">
                <p className="text-sm text-text-secondary">
                  确认要删除 <strong>{org.name}</strong> 吗？此操作不可恢复，且您必须是 owner。
                </p>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => { setDeleteConfirm(false); setError(''); }}
                    className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    onClick={() => deleteOrg.mutate()}
                    disabled={deleteOrg.isPending}
                    className="rounded bg-danger px-4 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
                  >
                    {deleteOrg.isPending ? '删除中…' : '确认删除'}
                  </button>
                </div>
              </div>
            )}
          </div>
        </>
      )}
    </section>
  );
}
