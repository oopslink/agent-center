import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useModelCatalog,
  useCreateModelCatalogEntry,
  useUpdateModelCatalogEntry,
  useDeleteModelCatalogEntry,
  useImportModelCatalog,
  type ModelCatalogEntry,
  type ModelCatalogFields,
} from '@/api/modelCatalog';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { ConfirmModal } from '@/components/ConfirmModal';

// Org settings "模型类目" panel (issue-93dd8daa ①): the org-level, user-managed
// model catalog — list + add/edit/delete + JSON bulk import (upsert|replace).
export default function OrgModelCatalog(): React.ReactElement {
  const { t } = useTranslation('admin');
  const catalog = useModelCatalog();
  const del = useDeleteModelCatalogEntry();
  const [formOpen, setFormOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [editing, setEditing] = useState<ModelCatalogEntry | null>(null);
  const [deleting, setDeleting] = useState<ModelCatalogEntry | null>(null);

  const confirmDelete = async () => {
    if (!deleting) return;
    try {
      await del.mutateAsync(deleting.id);
      setDeleting(null);
    } catch {
      /* surfaced via del.error */
    }
  };

  return (
    <section className="space-y-4" data-testid="page-OrgModelCatalog">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('modelCatalog.title')}</h1>
          <p className="text-xs text-text-muted">{t('modelCatalog.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
            onClick={() => setImportOpen(true)}
            data-testid="model-catalog-import-btn"
          >
            {t('modelCatalog.import.button')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
            data-testid="model-catalog-add-btn"
          >
            {t('modelCatalog.add')}
          </button>
        </div>
      </header>

      {catalog.isLoading && (
        <div className="space-y-2" data-testid="model-catalog-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}
      {catalog.isError && (
        <p className="text-sm text-danger" data-testid="model-catalog-error">
          {(catalog.error as Error).message}
        </p>
      )}
      {catalog.isSuccess && catalog.data.length === 0 && (
        <EmptyState testId="model-catalog-empty" title={t('modelCatalog.empty')} body={t('modelCatalog.emptyDesc')} />
      )}
      {catalog.isSuccess && catalog.data.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-border-base" data-testid="model-catalog-list">
          <table className="w-full text-sm">
            <thead className="bg-bg-subtle text-left text-xs uppercase tracking-wide text-text-muted">
              <tr>
                <th className="px-3 py-2">{t('modelCatalog.col.modelId')}</th>
                <th className="px-3 py-2">{t('modelCatalog.col.displayName')}</th>
                <th className="px-3 py-2">{t('modelCatalog.col.inputCost')}</th>
                <th className="px-3 py-2">{t('modelCatalog.col.outputCost')}</th>
                <th className="px-3 py-2">{t('modelCatalog.col.contextWindow')}</th>
                <th className="px-3 py-2">{t('modelCatalog.col.tier')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {catalog.data.map((e) => (
                <tr key={e.id} className="border-t border-border-base" data-testid="model-catalog-row" data-model-id={e.model_id}>
                  <td className="px-3 py-2 font-mono text-xs text-text-primary">{e.model_id}</td>
                  <td className="px-3 py-2 text-text-secondary">{e.display_name}</td>
                  <td className="px-3 py-2 text-text-secondary">{e.input_cost}</td>
                  <td className="px-3 py-2 text-text-secondary">{e.output_cost}</td>
                  <td className="px-3 py-2 text-text-secondary">{e.context_window}</td>
                  <td className="max-w-xs truncate px-3 py-2 text-text-muted" title={e.tier}>{e.tier}</td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    <button
                      type="button"
                      className="text-xs text-accent hover:underline"
                      onClick={() => {
                        setEditing(e);
                        setFormOpen(true);
                      }}
                      data-testid="model-catalog-edit"
                    >
                      {t('modelCatalog.edit')}
                    </button>
                    <button
                      type="button"
                      className="ml-3 text-xs text-danger hover:underline"
                      onClick={() => setDeleting(e)}
                      data-testid="model-catalog-delete"
                    >
                      {t('modelCatalog.delete')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {importOpen && <ImportModal onClose={() => setImportOpen(false)} />}

      {formOpen && <ModelCatalogFormModal entry={editing ?? undefined} onClose={() => setFormOpen(false)} />}

      <ConfirmModal
        open={!!deleting}
        title={t('modelCatalog.deleteTitle')}
        message={deleting ? t('modelCatalog.deleteConfirm', { name: deleting.model_id }) : ''}
        confirmLabel={t('modelCatalog.delete')}
        busy={del.isPending}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(null)}
      />
    </section>
  );
}

const EMPTY: ModelCatalogFields = { model_id: '', display_name: '', input_cost: 0, output_cost: 0, context_window: 0, tier: '' };

function ModelCatalogFormModal({ entry, onClose }: { entry?: ModelCatalogEntry; onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateModelCatalogEntry();
  const update = useUpdateModelCatalogEntry(entry?.id ?? '');
  const [f, setF] = useState<ModelCatalogFields>(
    entry
      ? {
          model_id: entry.model_id,
          display_name: entry.display_name,
          input_cost: entry.input_cost,
          output_cost: entry.output_cost,
          context_window: entry.context_window,
          tier: entry.tier,
        }
      : EMPTY,
  );
  const mut = entry ? update : create;
  const submit = async () => {
    try {
      await mut.mutateAsync(f);
      onClose();
    } catch {
      /* surfaced via mut.error */
    }
  };
  const num = (v: string) => (v === '' ? 0 : Number(v));

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="model-catalog-form">
      <div className="w-full max-w-md space-y-3 rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <h2 className="text-lg font-semibold text-text-primary">{entry ? t('modelCatalog.editTitle') : t('modelCatalog.addTitle')}</h2>
        <label className="block text-xs text-text-secondary">
          {t('modelCatalog.col.modelId')}
          <input className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.model_id} onChange={(e) => setF({ ...f, model_id: e.target.value })} data-testid="mc-field-model_id" />
        </label>
        <label className="block text-xs text-text-secondary">
          {t('modelCatalog.col.displayName')}
          <input className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} data-testid="mc-field-display_name" />
        </label>
        <div className="flex gap-3">
          <label className="block flex-1 text-xs text-text-secondary">
            {t('modelCatalog.col.inputCost')}
            <input type="number" step="any" min="0" className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.input_cost} onChange={(e) => setF({ ...f, input_cost: num(e.target.value) })} data-testid="mc-field-input_cost" />
          </label>
          <label className="block flex-1 text-xs text-text-secondary">
            {t('modelCatalog.col.outputCost')}
            <input type="number" step="any" min="0" className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.output_cost} onChange={(e) => setF({ ...f, output_cost: num(e.target.value) })} data-testid="mc-field-output_cost" />
          </label>
          <label className="block flex-1 text-xs text-text-secondary">
            {t('modelCatalog.col.contextWindow')}
            <input type="number" min="0" className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.context_window} onChange={(e) => setF({ ...f, context_window: num(e.target.value) })} data-testid="mc-field-context_window" />
          </label>
        </div>
        <label className="block text-xs text-text-secondary">
          {t('modelCatalog.col.tier')}
          <textarea rows={2} className="mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm" value={f.tier} onChange={(e) => setF({ ...f, tier: e.target.value })} data-testid="mc-field-tier" />
        </label>
        {mut.isError && <p className="text-xs text-danger" data-testid="mc-form-error">{(mut.error as Error).message}</p>}
        <div className="flex justify-end gap-2 pt-1">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>{t('modelCatalog.cancel')}</button>
          <button type="button" className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50" onClick={() => void submit()} disabled={mut.isPending || f.model_id.trim() === ''} data-testid="mc-form-save">{t('modelCatalog.save')}</button>
        </div>
      </div>
    </div>
  );
}

// ImportModal is the standalone "Bulk import (JSON)" dialog opened from the
// top-right toolbar button. It parses/validates the pasted JSON client-side
// (a parse failure never hits the server), then reuses useImportModelCatalog —
// the same batch endpoint the rest of the page writes through. The backend
// validates the whole batch, so a rejection means 0 imported / all failed.
function ImportModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
  const imp = useImportModelCatalog();
  const [json, setJson] = useState('');
  const [mode, setMode] = useState<'upsert' | 'replace'>('upsert');
  const [parseError, setParseError] = useState('');
  const [total, setTotal] = useState(0);

  const run = async () => {
    setParseError('');
    let parsed: unknown;
    try {
      parsed = JSON.parse(json);
    } catch (err) {
      setParseError(t('modelCatalog.import.parseError', { detail: (err as Error).message }));
      return;
    }
    if (!Array.isArray(parsed)) {
      setParseError(t('modelCatalog.import.notArray'));
      return;
    }
    setTotal(parsed.length);
    try {
      await imp.mutateAsync({ json, mode });
    } catch {
      /* surfaced via imp.error */
    }
  };

  const failed = total - (imp.data?.imported ?? 0);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="model-catalog-import">
      <div className="w-full max-w-lg space-y-3 rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <h2 className="text-lg font-semibold text-text-primary">{t('modelCatalog.import.title')}</h2>
        <p className="text-xs text-text-muted">{t('modelCatalog.import.help')}</p>
        <textarea
          rows={8}
          className="w-full rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-xs"
          placeholder='[{"model_id":"opus","input_cost":15,"output_cost":75,"context_window":200000,"tier":"hardest tasks"}]'
          value={json}
          onChange={(e) => setJson(e.target.value)}
          data-testid="model-catalog-import-json"
        />
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-1 text-xs text-text-secondary">
            <input type="radio" checked={mode === 'upsert'} onChange={() => setMode('upsert')} data-testid="model-catalog-import-upsert" />
            {t('modelCatalog.import.upsert')}
          </label>
          <label className="flex items-center gap-1 text-xs text-text-secondary">
            <input type="radio" checked={mode === 'replace'} onChange={() => setMode('replace')} data-testid="model-catalog-import-replace" />
            {t('modelCatalog.import.replace')}
          </label>
        </div>
        {parseError && <p className="text-xs text-danger" data-testid="model-catalog-import-parse-error">{parseError}</p>}
        {imp.isError && (
          <p className="text-xs text-danger" data-testid="model-catalog-import-error">
            {t('modelCatalog.import.failed', { count: total })} {(imp.error as Error).message}
          </p>
        )}
        {imp.isSuccess && (
          <p className="text-xs text-status-emerald-fg" data-testid="model-catalog-import-ok">
            {t('modelCatalog.import.result', { imported: imp.data.imported, failed })}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose} data-testid="model-catalog-import-close">
            {t('modelCatalog.import.close')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            onClick={() => void run()}
            disabled={imp.isPending || json.trim() === ''}
            data-testid="model-catalog-import-run"
          >
            {t('modelCatalog.import.run')}
          </button>
        </div>
      </div>
    </div>
  );
}
