import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useTemplates,
  useTemplate,
  useCreateTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
} from '@/api/templates';
import type { Template } from '@/api/types';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { ConfirmModal } from '@/components/ConfirmModal';

export default function OrgTemplates(): React.ReactElement {
  const { t } = useTranslation('admin');
  const templates = useTemplates();
  const del = useDeleteTemplate();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Template | null>(null);
  const [deleting, setDeleting] = useState<Template | null>(null);
  // Multiple templates can be expanded at once — track the set of expanded ids
  // (each card renders its own content panel inline; no single detached panel).
  const [expandedIds, setExpandedIds] = useState<Set<string>>(() => new Set());
  const toggleExpanded = (id: string) =>
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const openAdd = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (tpl: Template) => {
    setEditing(tpl);
    setFormOpen(true);
  };

  const confirmDelete = async () => {
    if (!deleting) return;
    try {
      await del.mutateAsync(deleting.id);
      setExpandedIds((prev) => {
        if (!prev.has(deleting.id)) return prev;
        const next = new Set(prev);
        next.delete(deleting.id);
        return next;
      });
      setDeleting(null);
    } catch {
      // surfaced via del.error
    }
  };

  return (
    <section className="space-y-4" data-testid="page-OrgTemplates">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('templates.title')}</h1>
          <p className="text-xs text-text-muted">
            {t('templates.subtitle')}
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={openAdd}
          data-testid="templates-add-btn"
        >
          {t('templates.addTemplate')}
        </button>
      </header>

      {templates.isLoading && (
        <div className="space-y-2" data-testid="templates-loading">
          <Skeleton height="4rem" />
          <Skeleton height="4rem" />
        </div>
      )}
      {templates.isError && (
        <p className="text-sm text-danger" data-testid="templates-error">
          {(templates.error as Error).message}
        </p>
      )}
      {templates.isSuccess && templates.data.length === 0 && (
        <EmptyState
          testId="templates-empty"
          title={t('templates.empty')}
          body={t('templates.emptyDesc')}
        />
      )}
      {templates.isSuccess && templates.data.length > 0 && (
        <ul className="space-y-2" data-testid="templates-list">
          {templates.data.map((tpl) => (
            <TemplateCard
              key={tpl.id}
              template={tpl}
              expanded={expandedIds.has(tpl.id)}
              onView={() => toggleExpanded(tpl.id)}
              onEdit={() => openEdit(tpl)}
              onDelete={() => setDeleting(tpl)}
            />
          ))}
        </ul>
      )}

      {formOpen && (
        <TemplateFormModal template={editing ?? undefined} onClose={() => setFormOpen(false)} />
      )}

      <ConfirmModal
        open={!!deleting}
        title={t('templates.deleteTitle')}
        message={
          deleting
            ? t('templates.deleteConfirm', { name: deleting.name })
            : ''
        }
        confirmLabel={t('templates.delete')}
        busy={del.isPending}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(null)}
      />
    </section>
  );
}

function TemplateCard({
  template,
  expanded,
  onView,
  onEdit,
  onDelete,
}: {
  template: Template;
  expanded: boolean;
  onView: () => void;
  onEdit: () => void;
  onDelete: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <li
      className={`rounded-lg border bg-bg-elevated p-3 shadow-1 ${expanded ? 'border-accent' : 'border-border-base'}`}
      data-testid="template-card"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="mb-0.5 flex items-center gap-2">
            <span className="truncate font-semibold text-text-primary" data-testid="template-card-name">
              {template.name}
            </span>
            {template.builtin && (
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-muted">
                {t('templates.builtinBadge')}
              </span>
            )}
          </div>
          {template.description && (
            <p className="text-xs text-text-secondary" data-testid="template-card-description">
              {template.description}
            </p>
          )}
        </div>
      </div>
      <div className="mt-2 flex justify-end gap-2">
        <button
          type="button"
          className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          onClick={onView}
          data-testid="template-card-view"
        >
          {expanded ? t('templates.hide') : t('templates.view')}
        </button>
        {!template.builtin && (
          <>
            <button
              type="button"
              className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
              onClick={onEdit}
              data-testid="template-card-edit"
            >
              {t('templates.edit')}
            </button>
            <button
              type="button"
              className="rounded border border-danger px-2 py-0.5 text-xs text-danger hover:bg-bg-subtle"
              onClick={onDelete}
              data-testid="template-card-delete"
            >
              {t('templates.delete')}
            </button>
          </>
        )}
      </div>
      {/* Content view lives INSIDE the card (attached below the actions), and
          multiple cards can be expanded at once. */}
      {expanded && <TemplateContentPanel template={template} />}
    </li>
  );
}

function TemplateContentPanel({ template }: { template: Template }): React.ReactElement {
  const { t } = useTranslation('admin');
  // The list DTO omits `content`; fetch the detail to render the full body.
  const detail = useTemplate(template.id);
  const content = detail.data?.content ?? template.content ?? '';
  return (
    <div className="mt-3 border-t border-border-base pt-3" data-testid="template-content-panel">
      {detail.isLoading && !content ? (
        <p className="text-xs text-text-muted" data-testid="template-content-loading">
          {t('templates.loading')}
        </p>
      ) : (
        <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded border border-border-base bg-bg-subtle px-3 py-2 font-mono text-xs text-text-secondary">
          {content}
        </pre>
      )}
    </div>
  );
}

function TemplateFormModal({
  template,
  onClose,
}: {
  template?: Template;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const isEdit = !!template;
  const create = useCreateTemplate();
  const update = useUpdateTemplate(template?.id ?? '');
  const mutation = isEdit ? update : create;

  // The list DTO omits `content`, so on edit we fetch the detail and hydrate the
  // textarea from it. Until it loads we block Save — otherwise submitting the
  // empty initial value would wipe the stored content.
  const detail = useTemplate(isEdit ? (template?.id ?? '') : '');
  const [name, setName] = useState(template?.name ?? '');
  const [description, setDescription] = useState(template?.description ?? '');
  const [content, setContent] = useState(template?.content ?? '');
  const hydrated = useRef(!isEdit);
  useEffect(() => {
    if (isEdit && detail.data && !hydrated.current) {
      hydrated.current = true;
      setContent(detail.data.content ?? '');
    }
  }, [isEdit, detail.data]);

  const contentLoading = isEdit && !hydrated.current && detail.isLoading;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (contentLoading) return;
    mutation.mutate(
      { name: name.trim(), description: description.trim(), content },
      { onSuccess: () => onClose() },
    );
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-lg rounded-xl border border-border-base bg-bg-elevated p-6 shadow-3">
        <h2 className="mb-4 text-base font-semibold text-text-primary">
          {isEdit ? t('templates.editTitle') : t('templates.addTitle')}
        </h2>
        {mutation.isError && (
          <p className="mb-3 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger" role="alert">
            {(mutation.error as Error).message}
          </p>
        )}
        <form onSubmit={handleSubmit} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="tpl-name" className="block text-sm text-text-primary">
              {t('templates.name')}
            </label>
            <input
              id="tpl-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('templates.namePlaceholder')}
              className="w-full rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
              autoFocus
              data-testid="template-form-name"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="tpl-desc" className="block text-sm text-text-primary">
              {t('templates.description')}
            </label>
            <input
              id="tpl-desc"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('templates.descriptionPlaceholder')}
              className="w-full rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
              data-testid="template-form-description"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="tpl-content" className="block text-sm text-text-primary">
              {t('templates.content')}
            </label>
            <textarea
              id="tpl-content"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={contentLoading ? t('templates.loading') : t('templates.contentPlaceholder')}
              rows={12}
              disabled={contentLoading}
              className="w-full rounded border border-border-base bg-bg-elevated px-3 py-2 font-mono text-sm text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              data-testid="template-form-content"
            />
          </div>
          <div className="flex justify-end gap-2 pt-1">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
            >
              {t('templates.cancel')}
            </button>
            <button
              type="submit"
              disabled={mutation.isPending || contentLoading}
              className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
              data-testid="template-form-submit"
            >
              {mutation.isPending ? t('templates.saving') : t('templates.save')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
