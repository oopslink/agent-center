// IssueEditModal — v2.8.1 (@oopslink directive, mirror of TaskEditModal #278):
// full Edit-Issue form that batch-saves title / description / status / tags in
// ONE atomic PATCH (PATCH /projects/{pid}/issues/{id} → Dev's #251 contract).
// Only the changed (dirty) fields are sent; the backend applies all-or-none.
// NO assignee field — Issues are not assignable (unlike Tasks).
import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useUpdateIssue } from '@/api/issues';
import type { Issue, IssueStatus } from '@/api/types';
import { useModalA11y } from './useModalA11y';
import { MAX_TAG_RUNES, MAX_TAGS, runeLength, validateTags } from './tagValidation';
import { tagColorFor } from './tagColors';

interface Props {
  projectId: string;
  issue: Pick<Issue, 'id' | 'title' | 'description' | 'status' | 'tags'>;
  onClose: () => void;
  onSaved?: () => void;
}

// All IssueStatus values — free-state model (any valid value selectable, no
// adjacency machinery), mirroring TaskEditModal's status select. Keep in sync
// with the IssueStatus union.
const ISSUE_STATUSES: IssueStatus[] = [
  'open',
  'in_progress',
  'resolved',
  'closed',
  'discarded',
  'reopened',
];

export function IssueEditModal({ projectId, issue, onClose, onSaved }: Props): React.ReactElement {
  const { t } = useTranslation('work');
  const [title, setTitle] = useState(issue.title ?? '');
  const [description, setDescription] = useState(issue.description ?? '');
  const [status, setStatus] = useState<IssueStatus>(issue.status ?? 'open');
  const [tags, setTags] = useState<string[]>(issue.tags ?? []);
  const [tagDraft, setTagDraft] = useState('');
  const [tagError, setTagError] = useState<string | null>(null);

  const update = useUpdateIssue(projectId, issue.id);
  // a11y: Escape closes + focus-trap (rendered = open).
  const containerRef = useModalA11y({ open: true, onClose });

  // commitTag: add the typed draft as a chip (Enter/comma). Trims, validates the
  // single tag (rune-16), dedups (no-op if already present), enforces the 10-cap.
  const commitTag = () => {
    const candidate = tagDraft.trim();
    if (candidate === '') {
      setTagDraft('');
      return;
    }
    if (runeLength(candidate) > MAX_TAG_RUNES) {
      setTagError(t('issue.edit.tagTooLong', { max: MAX_TAG_RUNES }));
      return;
    }
    if (tags.includes(candidate)) {
      // dedup — keep first; just clear the draft.
      setTagDraft('');
      setTagError(null);
      return;
    }
    if (tags.length >= MAX_TAGS) {
      setTagError(t('issue.edit.tagMax', { max: MAX_TAGS }));
      return;
    }
    setTags([...tags, candidate]);
    setTagDraft('');
    setTagError(null);
  };

  const removeTag = (tag: string) => {
    setTags(tags.filter((t) => t !== tag));
    setTagError(null);
  };

  const onTagKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commitTag();
    }
  };

  const tagsValidationError = useMemo(() => validateTags(tags), [tags]);

  const trimmedTitle = title.trim();
  // Dirty diff — compare against the original issue so we send ONLY changed fields.
  const origTags = issue.tags ?? [];
  const tagsChanged =
    tags.length !== origTags.length || tags.some((t, i) => t !== origTags[i]);
  const titleChanged = trimmedTitle !== (issue.title ?? '');
  const descChanged = description.trim() !== (issue.description ?? '');
  const statusChanged = status !== (issue.status ?? 'open');
  const anyDirty = titleChanged || descChanged || statusChanged || tagsChanged;

  const hasError = !!tagError || !!tagsValidationError;
  const canSubmit =
    trimmedTitle.length > 0 && anyDirty && !hasError && !update.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    // Build the dirty-only batch body. Wire keys match Dev's #251 contract:
    // {title?, description?, status?, tags?} — "description" (not "desc"),
    // NO assignee (Issues aren't assignable).
    const body: {
      title?: string;
      description?: string;
      status?: IssueStatus;
      tags?: string[];
    } = {};
    if (titleChanged) body.title = trimmedTitle;
    if (descChanged) body.description = description.trim();
    if (statusChanged) body.status = status;
    if (tagsChanged) body.tags = tags;
    try {
      await update.mutateAsync(body);
      onSaved?.();
      onClose();
    } catch {
      // Atomic = nothing applied. Surfaced via update.error; modal stays open.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="issue-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t('issue.edit.dialogAria')}
    >
      <form
        onSubmit={submit}
        className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{t('issue.edit.heading')}</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label={t('issue.edit.close')}
            data-testid="issue-edit-close"
          >
            X
          </button>
        </div>

        <div className="mb-3">
          <label htmlFor="issue-edit-title" className="mb-1 block text-xs font-medium text-text-primary">
            {t('issue.edit.titleLabel')}<span className="ml-1 text-danger">*</span>
          </label>
          <input
            id="issue-edit-title"
            data-testid="issue-edit-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>

        <div className="mb-3">
          <label
            htmlFor="issue-edit-description"
            className="mb-1 block text-xs font-medium text-text-primary"
          >
            {t('issue.edit.descriptionLabel')}
          </label>
          <textarea
            id="issue-edit-description"
            data-testid="issue-edit-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={5}
          />
        </div>

        <div className="mb-3">
          <label htmlFor="issue-edit-status" className="mb-1 block text-xs font-medium text-text-primary">
            {t('issue.edit.statusLabel')}
          </label>
          <select
            id="issue-edit-status"
            data-testid="issue-edit-status"
            className={inputClass}
            value={status}
            onChange={(e) => setStatus(e.target.value as IssueStatus)}
          >
            {ISSUE_STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`status.${s}`)}
              </option>
            ))}
          </select>
        </div>

        <div className="mb-3">
          <label htmlFor="issue-edit-tags-input" className="mb-1 block text-xs font-medium text-text-primary">
            {t('issue.edit.tagsLabel')}
          </label>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="issue-edit-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                  <button
                    type="button"
                    className="hover:opacity-70"
                    onClick={() => removeTag(tag)}
                    aria-label={t('issue.edit.removeTag', { tag })}
                    data-testid="issue-edit-tag-remove"
                  >
                    x
                  </button>
                </span>
              );
            })}
          </div>
          <input
            id="issue-edit-tags-input"
            data-testid="issue-edit-tags-input"
            className={`${inputClass} mt-1.5`}
            value={tagDraft}
            onChange={(e) => {
              setTagDraft(e.target.value);
              if (tagError) setTagError(null);
            }}
            onKeyDown={onTagKeyDown}
            placeholder={t('issue.edit.tagsPlaceholder')}
            aria-describedby="issue-edit-tags-hint"
          />
          <p id="issue-edit-tags-hint" className="mt-1 text-[0.6875rem] text-text-muted">
            {t('issue.edit.tagsHint', { maxTags: MAX_TAGS, maxRunes: MAX_TAG_RUNES })}
          </p>
          {(tagError || tagsValidationError) && (
            <p className="mt-1 text-xs text-danger" data-testid="issue-edit-tag-error">
              {tagError ?? tagsValidationError}
            </p>
          )}
        </div>

        {update.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="issue-edit-error">
            {(update.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="issue-edit-cancel"
          >
            {t('issue.edit.cancel')}
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="issue-edit-submit"
          >
            {update.isPending ? t('issue.edit.saving') : t('issue.edit.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
