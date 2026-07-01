// RepoFormModal — add / edit a WORKSPACE code repo (T575, issue-f980c8de).
// Credentials are configured ONLY here (write-only): on edit the field shows a
// placeholder reflecting whether a credential is already stored; leaving it blank
// keeps the existing one (the backend treats an omitted credential as unchanged).
import React, { useState } from 'react';
import { useTranslation, Trans } from 'react-i18next';
import {
  useCreateWorkspaceRepo,
  useUpdateWorkspaceRepo,
  type CreateWorkspaceRepoInput,
} from '@/api/repos';
import type { WorkspaceRepo } from '@/api/types';
import { REPO_PROVIDERS } from './repoDisplay';
import { useModalA11y } from './useModalA11y';

interface Props {
  /** Present = edit that repo; absent = create a new one. */
  repo?: WorkspaceRepo;
  onClose: () => void;
}

export function RepoFormModal({ repo, onClose }: Props): React.ReactElement {
  const { t } = useTranslation('admin');
  const isEdit = !!repo;
  const [label, setLabel] = useState(repo?.label ?? '');
  const [provider, setProvider] = useState(repo?.provider || 'github');
  const [description, setDescription] = useState(repo?.description ?? '');
  const [url, setUrl] = useState(repo?.url ?? '');
  const [defaultBranch, setDefaultBranch] = useState(repo?.default_branch ?? '');
  // Credential is write-only: "" means "leave unchanged" on edit (we only send it
  // when the operator types something). On create, "" means no credential.
  const [credential, setCredential] = useState('');

  const create = useCreateWorkspaceRepo();
  const update = useUpdateWorkspaceRepo(repo?.id ?? '');
  const containerRef = useModalA11y({ open: true, onClose });

  const busy = create.isPending || update.isPending;
  const error = (create.error ?? update.error) as Error | null;
  const trimmedLabel = label.trim();
  const trimmedUrl = url.trim();
  const canSubmit = trimmedLabel.length > 0 && trimmedUrl.length > 0 && !busy;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    const base: CreateWorkspaceRepoInput = {
      label: trimmedLabel,
      description: description.trim(),
      url: trimmedUrl,
      provider,
      default_branch: defaultBranch.trim(),
    };
    try {
      if (isEdit) {
        // Only send credential when the operator typed one (else keep existing).
        await update.mutateAsync(
          credential ? { ...base, credential } : base,
        );
      } else {
        await create.mutateAsync(credential ? { ...base, credential } : base);
      }
      onClose();
    } catch {
      // surfaced via `error`; keep modal open.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="repo-form-modal"
      role="dialog"
      aria-modal="true"
      aria-label={isEdit ? t('repos.form.editTitle') : t('repos.form.addTitle')}
    >
      <form
        onSubmit={submit}
        className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{isEdit ? t('repos.form.editTitle') : t('repos.form.addTitle')}</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label={t('repos.form.close')}
            data-testid="repo-form-close"
          >
            <span aria-hidden="true">X</span>
          </button>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('repos.form.label')} required htmlFor="repo-form-label">
            <input
              id="repo-form-label"
              data-testid="repo-form-label"
              className={inputClass}
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t('repos.form.labelPlaceholder')}
            />
          </Field>
          <Field label={t('repos.form.provider')} htmlFor="repo-form-provider">
            <select
              id="repo-form-provider"
              data-testid="repo-form-provider"
              className={inputClass}
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
            >
              {REPO_PROVIDERS.map((p) => (
                <option key={p} value={p}>
                  {p === 'github' ? t('repos.form.providerGithub') : t('repos.form.providerGit')}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Field label={t('repos.form.description')} hint={t('repos.form.descriptionHint')} htmlFor="repo-form-description">
          <input
            id="repo-form-description"
            data-testid="repo-form-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t('repos.form.descriptionPlaceholder')}
          />
        </Field>

        <Field label={t('repos.form.url')} required htmlFor="repo-form-url">
          <input
            id="repo-form-url"
            data-testid="repo-form-url"
            className={inputClass}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={t('repos.form.urlPlaceholder')}
          />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('repos.form.defaultBranch')} htmlFor="repo-form-branch">
            <input
              id="repo-form-branch"
              data-testid="repo-form-default-branch"
              className={inputClass}
              value={defaultBranch}
              onChange={(e) => setDefaultBranch(e.target.value)}
              placeholder={t('repos.form.defaultBranchPlaceholder')}
            />
          </Field>
          <Field
            label={t('repos.form.credential')}
            hint={isEdit ? t('repos.form.credentialHintEdit') : t('repos.form.credentialHintCreate')}
            htmlFor="repo-form-credential"
          >
            <input
              id="repo-form-credential"
              data-testid="repo-form-credential"
              type="password"
              autoComplete="new-password"
              className={inputClass}
              value={credential}
              onChange={(e) => setCredential(e.target.value)}
              placeholder={isEdit && repo?.has_credential ? t('repos.form.credentialPlaceholderStored') : t('repos.form.credentialPlaceholder')}
            />
          </Field>
        </div>

        <p className="mb-3 -mt-1 text-xs text-text-muted" data-testid="repo-form-credential-help">
          <Trans
            t={t}
            i18nKey="repos.form.credentialHelp"
            components={{
              tokenLink: (
                <a
                  href="https://github.com/settings/personal-access-tokens/new?name=agent-center+repo+read&contents=read"
                  target="_blank"
                  rel="noreferrer"
                  className="text-accent hover:underline"
                />
              ),
              contents: <span className="text-text-secondary" />,
              prs: <span className="text-text-secondary" />,
            }}
          />
        </p>

        {error && (
          <p className="mb-3 text-xs text-danger" data-testid="repo-form-error">
            {error.message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="repo-form-cancel"
          >
            {t('repos.form.cancel')}
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="repo-form-save"
          >
            {busy ? t('repos.form.saving') : t('repos.form.save')}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({
  label,
  hint,
  required,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  htmlFor?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3">
      <label htmlFor={htmlFor} className="mb-1 block text-xs font-medium text-text-primary">
        {label}
        {required && <span className="ml-1 text-danger">*</span>}
      </label>
      {children}
      {hint && <p className="mt-1 text-[0.6875rem] text-text-muted">{hint}</p>}
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
