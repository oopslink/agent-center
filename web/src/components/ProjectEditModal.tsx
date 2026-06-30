import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { useUpdateProject, type Project } from '@/api/projects';

const editInputClass =
  'mb-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// ProjectEditModal — edit a project's name + description (PATCH /projects/{id}).
// Extracted as a shared component (T139) so the Projects LIST card's "Edit"
// shortcut can open the SAME edit affordance that ProjectDetail offers, without
// duplicating the form or reaching into ProjectDetail's internals. Only changed
// fields are sent (so a no-op save patches nothing). Errors surface inline.
export function ProjectEditModal({
  project: p,
  onClose,
}: {
  project: Project;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const [name, setName] = useState(p.name);
  const [description, setDescription] = useState(p.description ?? '');
  const update = useUpdateProject(p.id);

  const submit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault();
    const fields: { name?: string; description?: string } = {};
    if (name !== p.name) fields.name = name;
    if (description !== (p.description ?? '')) fields.description = description;
    try {
      await update.mutateAsync(fields);
      onClose();
    } catch {
      // surfaced inline below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="project-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t('project.edit.ariaLabel')}
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <h2 className="mb-4 text-lg font-semibold">{t('project.edit.title')}</h2>
        <label className="mb-2 block text-xs font-medium">{t('project.edit.nameLabel')}</label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={editInputClass}
          data-testid="project-edit-name"
          autoFocus
        />
        <label className="mb-2 mt-3 block text-xs font-medium">{t('project.edit.descriptionLabel')}</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={editInputClass}
          data-testid="project-edit-description"
        />
        {update.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="project-edit-error">
            {(update.error as Error).message}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
          >
            {t('project.edit.cancel')}
          </button>
          <button
            type="submit"
            disabled={update.isPending}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="project-edit-save"
          >
            {update.isPending ? t('project.edit.saving') : t('project.edit.save')}
          </button>
        </div>
      </form>
    </div>
  );
}
