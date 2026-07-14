// Team Templates (/organizations/:slug/teams/templates) — template catalog.
// Two authoring entry points in the header: Import JSON (cross-org) and Extract
// from an existing team (the second extract entry point; the first is a team's
// detail header). Cards → template detail; each card can Instantiate / Export.
import { useState } from 'react';
import type React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useOptionalOrgContext } from '@/OrgContext';
import {
  exportTemplateEnvelope,
  useImportTemplate,
  useTeamTemplates,
  useTeams,
  type TeamTemplate,
} from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { ExtractModal } from '@/components/teams/ExtractModal';
import { InstantiateModal } from '@/components/teams/InstantiateModal';
import { btnGhost, btnPrimary, btnSm, btnSmPrimary, Field, inputCls, ModalShell } from '@/components/teams/kit';
import { ExtractIcon, ImportIcon } from '@/components/teams/teamsUi';

export default function TeamTemplates(): React.ReactElement {
  const { t } = useTranslation('teams');
  const templates = useTeamTemplates();
  const teams = useTeams();
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [importing, setImporting] = useState(false);
  const [extracting, setExtracting] = useState(false);
  const [instantiate, setInstantiate] = useState<TeamTemplate | null>(null);

  const openDetail = (id: string) => navigate(`${orgBase}/teams/templates/${id}`);
  const sourceTeam = teams.data?.[0] ?? null;

  return (
    <section className="space-y-4" data-testid="page-TeamTemplates">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('templates.title')}</h1>
          <p className="mt-1 font-mono text-xs text-text-muted">{orgBase || '/organizations/:slug'}/teams/templates</p>
        </div>
        <div className="flex gap-2.5">
          <button type="button" className={btnGhost} data-testid="templates-import" onClick={() => setImporting(true)}>
            <ImportIcon className="h-4 w-4" /> {t('templates.import')}
          </button>
          <button type="button" className={btnPrimary} data-testid="templates-extract" onClick={() => setExtracting(true)}>
            <ExtractIcon className="h-4 w-4" /> {t('templates.extractFromTeam')}
          </button>
        </div>
      </header>

      {templates.isLoading && (
        <div className="grid gap-3.5 md:grid-cols-3">
          <Skeleton height="10rem" />
          <Skeleton height="10rem" />
          <Skeleton height="10rem" />
        </div>
      )}
      {templates.isError && <p className="text-sm text-danger">{(templates.error as Error).message}</p>}
      {templates.isSuccess && templates.data.length === 0 && (
        <EmptyState title={t('templates.emptyTitle')} body={t('templates.emptyBody')} testId="templates-empty" />
      )}

      {templates.isSuccess && templates.data.length > 0 && (
        <div className="grid gap-3.5 md:grid-cols-3" data-testid="templates-grid">
          {templates.data.map((tpl) => (
            <div
              key={tpl.id}
              data-testid={`template-card-${tpl.id}`}
              className="relative cursor-pointer rounded-xl border border-border-base bg-bg-elevated p-4 shadow-1 hover:border-border-strong"
              onClick={() => openDetail(tpl.id)}
            >
              <div className="absolute right-3.5 top-3.5 font-mono text-[0.625rem] text-text-muted">{tpl.version_label}</div>
              <h3 className="text-[0.95rem] font-semibold text-text-primary">{tpl.name}</h3>
              <div className="mt-0.5 font-mono text-[0.6875rem] text-text-muted">{tpl.source}</div>
              <div className="my-3.5 flex flex-wrap gap-1.5">
                {tpl.roles.map((r) => (
                  <span key={r.role} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 text-[0.65rem] text-text-secondary">
                    {r.role}
                    {r.count > 1 ? `×${r.count}` : ''}
                  </span>
                ))}
              </div>
              <div className="flex gap-2 border-t border-border-base pt-3">
                <button
                  type="button"
                  className={btnSmPrimary}
                  data-testid={`template-instantiate-${tpl.id}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    setInstantiate(tpl);
                  }}
                >
                  {t('templates.instantiate')}
                </button>
                <button
                  type="button"
                  className={btnSm}
                  onClick={(e) => {
                    e.stopPropagation();
                    openDetail(tpl.id);
                  }}
                >
                  {t('templates.details')}
                </button>
                <button
                  type="button"
                  className={btnSm}
                  data-testid={`template-export-${tpl.id}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    downloadJson(`${tpl.name}.team-template.json`, exportTemplateEnvelope(tpl));
                  }}
                >
                  {t('templates.export')}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {importing && <ImportModal onClose={() => setImporting(false)} onImported={openDetail} />}
      {extracting && sourceTeam && (
        <ExtractModal team={sourceTeam} onClose={() => setExtracting(false)} onSaved={() => setExtracting(false)} />
      )}
      {instantiate && (
        <InstantiateModal
          template={instantiate}
          onClose={() => setInstantiate(null)}
          onInstantiated={(id) => navigate(`${orgBase}/teams/${id}`)}
        />
      )}
    </section>
  );
}

function ImportModal({ onClose, onImported }: { onClose: () => void; onImported: (id: string) => void }): React.ReactElement {
  const { t } = useTranslation('teams');
  const [text, setText] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const importT = useImportTemplate();

  const submit = async () => {
    setErr(null);
    let doc: Record<string, unknown>;
    try {
      doc = JSON.parse(text);
    } catch {
      setErr(t('templates.importModal.invalidJson'));
      return;
    }
    try {
      const tmpl = await importT.mutateAsync(doc);
      onClose();
      onImported(tmpl.id);
    } catch (e) {
      setErr((e as Error).message);
    }
  };

  return (
    <ModalShell
      open
      onClose={onClose}
      testId="import-modal"
      title={t('templates.importModal.title')}
      subtitle={t('templates.importModal.subtitle')}
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="button" className={btnPrimary} data-testid="import-submit" disabled={!text.trim() || importT.isPending} onClick={submit}>
              {importT.isPending ? t('templates.importModal.importing') : t('templates.importModal.submit')}
            </button>
          </div>
        </>
      }
    >
      <Field label={t('templates.importModal.jsonLabel')} required>
        <textarea
          className={`${inputCls} min-h-[180px] font-mono text-xs`}
          value={text}
          data-testid="import-json"
          placeholder='{"format":"team-template/v1","name":"…","roles":[…]}'
          onChange={(e) => setText(e.target.value)}
        />
      </Field>
      {err && <p className="text-xs text-danger" data-testid="import-error">{err}</p>}
    </ModalShell>
  );
}

// Trigger a client-side JSON download (export is a client-held artifact in P1).
function downloadJson(filename: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
