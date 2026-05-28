import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOrgs, orgApi } from '@/api/auth';
import { ApiError } from '@/api/client';

export default function OrgSettings(): React.ReactElement {
  const orgs = useOrgs();
  const qc = useQueryClient();
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  const org = orgs.data?.[0]; // current org (single-org view; multi-org routing in FE-6)

  const deleteOrg = useMutation({
    mutationFn: () => orgApi.delete(org!.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setDeleteConfirm(false);
      setSuccess('组织已删除');
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError('删除失败，请稍后重试');
      }
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
          <div className="bg-bg-elevated border border-border rounded-lg p-4 space-y-2">
            <h3 className="text-sm font-semibold text-text-primary">组织信息</h3>
            <dl className="space-y-1">
              <div className="flex gap-2 text-sm">
                <dt className="text-text-muted w-16 flex-shrink-0">名称</dt>
                <dd className="text-text-primary">{org.name}</dd>
              </div>
              <div className="flex gap-2 text-sm">
                <dt className="text-text-muted w-16 flex-shrink-0">Slug</dt>
                <dd className="text-text-secondary font-mono text-xs">{org.slug}</dd>
              </div>
            </dl>
            <p className="text-xs text-text-muted pt-1">
              组织名称 / Slug 编辑功能将在 FE-6 中启用。
            </p>
          </div>

          <div className="bg-bg-elevated border border-border rounded-lg p-4">
            <h3 className="text-sm font-semibold text-danger mb-2">危险操作</h3>
            {error && (
              <div role="alert" className="mb-2 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
                {error}
              </div>
            )}
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
                  确认要删除 <strong>{org.name}</strong> 吗？此操作不可恢复。
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
