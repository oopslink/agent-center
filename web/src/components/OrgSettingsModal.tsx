import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { useOrgs, orgApi } from '@/api/auth';
import { ApiError } from '@/api/client';

function validateSlug(v: string, t: (key: string) => string): string {
  if (v.length < 3) return t('orgSettings.modal.slugTooShort');
  if (v.length > 40) return t('orgSettings.modal.slugTooLong');
  if (!/^[a-z0-9-]+$/.test(v)) return t('orgSettings.modal.slugInvalidChars');
  if (/^-|-$/.test(v)) return t('orgSettings.modal.slugHyphenEdge');
  return '';
}

// v2.7 #186-6: organization settings as a per-org modal opened from the org
// switcher gear, instead of a standalone /org/settings page. It resolves the
// target org by id (not the URL slug) so any org in the switcher can be
// edited without first navigating to it.
export function OrgSettingsModal({
  orgId,
  onClose,
}: {
  orgId: string;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const orgs = useOrgs();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');

  const org = (orgs.data ?? []).find((o) => o.id === orgId);

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
      setSuccess(t('orgSettings.modal.updateSuccess'));
      setError('');
      setTimeout(() => setSuccess(''), 3000);
      // Slug change moves the org's URL — navigate to its new root.
      if (slugChanged) {
        navigate(`/organizations/${slug}`);
        onClose();
      }
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError(t('orgSettings.modal.updateError'));
    },
  });

  const deleteOrg = useMutation({
    mutationFn: () => orgApi.delete(org!.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      setDeleteConfirm(false);
      onClose();
      navigate('/');
    },
    onError: (err) => {
      if (err instanceof ApiError) setError(err.message);
      else setError(t('orgSettings.modal.deleteError'));
    },
  });

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (org && slug !== org.slug) {
      const slugErr = validateSlug(slug, t);
      if (slugErr) { setError(slugErr); return; }
    }
    if (!name.trim()) { setError(t('orgSettings.modal.nameRequired')); return; }
    save.mutate();
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="org-settings-title"
      data-testid="org-settings-modal"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-md rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)] space-y-5">
        <div className="flex items-center justify-between">
          <h2 id="org-settings-title" className="text-base font-semibold text-text-primary">
            {t('orgSettings.modal.title')}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('orgSettings.modal.close')}
            className="text-text-muted hover:text-text-primary"
            data-testid="org-settings-cancel"
          >
            <CloseIcon />
          </button>
        </div>

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

        {orgs.isLoading && !org && <p className="text-sm text-text-muted">{t('orgSettings.modal.loading')}</p>}
        {!orgs.isLoading && !org && (
          <p className="text-sm text-text-muted" data-testid="org-settings-missing">{t('orgSettings.modal.notFound')}</p>
        )}

        {org && (
          <>
            <form onSubmit={handleSave} noValidate className="space-y-3">
              <div className="space-y-1">
                <label htmlFor="org-settings-name-input" className="block text-xs text-text-muted">{t('orgSettings.modal.nameLabel')}</label>
                <input
                  id="org-settings-name-input"
                  data-testid="org-settings-name"
                  type="text"
                  value={name}
                  maxLength={80}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="org-settings-slug-input" className="block text-xs text-text-muted">{t('orgSettings.modal.slugLabel')}</label>
                <input
                  id="org-settings-slug-input"
                  data-testid="org-settings-slug"
                  type="text"
                  value={slug}
                  maxLength={40}
                  onChange={(e) => setSlug(e.target.value)}
                  className="w-full rounded border border-border px-3 py-1.5 text-sm font-mono bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="org-settings-desc-input" className="block text-xs text-text-muted">{t('orgSettings.modal.descriptionLabel')}</label>
                <textarea
                  id="org-settings-desc-input"
                  data-testid="org-settings-description"
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
                  data-testid="org-settings-save"
                  className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
                >
                  {save.isPending ? t('orgSettings.modal.saving') : t('orgSettings.modal.save')}
                </button>
              </div>
            </form>

            <div className="border-t border-border pt-4">
              <h3 className="text-sm font-semibold text-danger mb-2">{t('orgSettings.modal.dangerZone')}</h3>
              {!deleteConfirm ? (
                <button
                  type="button"
                  onClick={() => setDeleteConfirm(true)}
                  data-testid="org-settings-delete"
                  className="rounded border border-danger/50 px-4 py-1.5 text-sm text-danger hover:bg-danger/10"
                >
                  {t('orgSettings.modal.deleteOrganization')}
                </button>
              ) : (
                <div className="space-y-2" data-testid="org-settings-delete-confirm">
                  <p className="text-sm text-text-secondary">
                    {t('orgSettings.modal.deleteConfirmPrefix')}<strong>{org.name}</strong>{t('orgSettings.modal.deleteConfirmSuffix')}
                  </p>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      onClick={() => { setDeleteConfirm(false); setError(''); }}
                      className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
                    >
                      {t('orgSettings.modal.cancel')}
                    </button>
                    <button
                      type="button"
                      onClick={() => deleteOrg.mutate()}
                      disabled={deleteOrg.isPending}
                      data-testid="org-settings-delete-confirm-button"
                      className="rounded bg-danger px-4 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
                    >
                      {deleteOrg.isPending ? t('orgSettings.modal.deleting') : t('orgSettings.modal.confirmDelete')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function CloseIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M5 5l10 10M15 5L5 15" strokeLinecap="round" />
    </svg>
  );
}
