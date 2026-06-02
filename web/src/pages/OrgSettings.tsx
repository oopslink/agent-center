import React, { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOrgs, orgApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';

function validateSlug(v: string): string {
  if (v.length < 3) return 'Slug must be at least 3 characters';
  if (v.length > 40) return 'Slug must be at most 40 characters';
  if (!/^[a-z0-9-]+$/.test(v)) return 'Slug may only contain [a-z0-9-]';
  if (/^-|-$/.test(v)) return 'Slug cannot start or end with a hyphen';
  return '';
}

export default function OrgSettings(): React.ReactElement {
  const orgs = useOrgs();
  const orgCtx = useOptionalOrgContext();
  const qc = useQueryClient();
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  // Edit drafts.
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');

  // v2.6 multi-org: use the org from URL slug (OrgGuard context), not "first org".
  const org = orgCtx
    ? (orgs.data ?? []).find((o) => o.slug === orgCtx.slug)
    : orgs.data?.[0];

  useEffect(() => {
    if (org) {
      setName(org.name);
      setSlug(org.slug);
      setDescription(org.description ?? '');
    }
  }, [org]);

  const dirty =
    org !== undefined &&
    (name.trim() !== org.name || slug !== org.slug || description !== (org.description ?? ''));

  const save = useMutation({
    mutationFn: () => {
      const payload: { name?: string; slug?: string; description?: string } = {};
      if (org) {
        if (name.trim() !== org.name) payload.name = name.trim();
        if (slug !== org.slug) payload.slug = slug;
        if (description !== (org.description ?? '')) payload.description = description;
      }
      return orgApi.update(org!.id, payload);
    },
    onSuccess: () => {
      const slugChanged = org && slug !== org.slug;
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setSuccess('Organization info updated');
      setError('');
      setTimeout(() => setSuccess(''), 3000);
      // Slug change moves the URL — redirect to the new slug.
      if (slugChanged) {
        setTimeout(() => { window.location.href = `/organizations/${slug}/org/settings`; }, 600);
      }
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError('Update failed');
    },
  });

  const deleteOrg = useMutation({
    mutationFn: () => orgApi.delete(org!.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setDeleteConfirm(false);
      setSuccess('Organization deleted');
      setTimeout(() => { window.location.href = '/'; }, 800);
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError('Delete failed, please try again');
    },
  });

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (org && slug !== org.slug) {
      const slugErr = validateSlug(slug);
      if (slugErr) { setError(slugErr); return; }
    }
    if (!name.trim()) { setError('Please enter an organization name'); return; }
    save.mutate();
  };

  return (
    <section className="space-y-6 max-w-md" data-testid="page-OrgSettings">
      <h2 className="text-xl font-semibold text-text-primary">Organization Settings</h2>

      {success && (
        <div role="status" className="rounded-md bg-success/10 border border-success/30 px-3 py-2 text-sm text-success">
          {success}
        </div>
      )}
      {error && (
        <div role="alert" className="rounded-md bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      {orgs.isLoading && <p className="text-sm text-text-muted">Loading…</p>}

      {org && (
        <>
          <form onSubmit={handleSave} noValidate className="bg-bg-elevated border border-border rounded-lg p-4 space-y-3">
            <h3 className="text-sm font-semibold text-text-primary">Organization Info</h3>
            <div className="space-y-1">
              <label htmlFor="org-name" className="block text-xs text-text-muted">Name</label>
              <input
                id="org-name"
                type="text"
                value={name}
                maxLength={80}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="org-slug" className="block text-xs text-text-muted">Slug (changing this changes the URL)</label>
              <input
                id="org-slug"
                type="text"
                value={slug}
                maxLength={40}
                onChange={(e) => setSlug(e.target.value)}
                className="w-full rounded border border-border px-3 py-1.5 text-sm font-mono bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="org-desc" className="block text-xs text-text-muted">Description</label>
              <textarea
                id="org-desc"
                value={description}
                rows={3}
                onChange={(e) => setDescription(e.target.value)}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
            <div className="flex justify-end">
              <button
                type="submit"
                disabled={save.isPending || !dirty}
                className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
              >
                {save.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
          </form>

          <div className="bg-bg-elevated border border-border rounded-lg p-4">
            <h3 className="text-sm font-semibold text-danger mb-2">Danger Zone</h3>
            {!deleteConfirm ? (
              <button
                type="button"
                onClick={() => setDeleteConfirm(true)}
                className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10"
              >
                Delete Organization
              </button>
            ) : (
              <div className="space-y-2">
                <p className="text-sm text-text-secondary">
                  Delete <strong>{org.name}</strong>? This cannot be undone, and you must be an owner.
                </p>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => { setDeleteConfirm(false); setError(''); }}
                    className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onClick={() => deleteOrg.mutate()}
                    disabled={deleteOrg.isPending}
                    className="rounded bg-danger px-4 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
                  >
                    {deleteOrg.isPending ? 'Deleting…' : 'Confirm delete'}
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
