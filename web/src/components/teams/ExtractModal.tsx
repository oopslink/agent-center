// Team WebUI — Extract → Template modal (the curation gate). Scans a source team
// for suspected proprietary tokens; the human must curate EVERY finding (no bulk
// select, by design). The gate BLOCKS save while any high-risk token is kept.
// Reached from two entry points: a team detail header and the Templates page.
import { useEffect, useMemo, useState } from 'react';
import type React from 'react';
import type { TFunction } from 'i18next';
import { Trans, useTranslation } from 'react-i18next';
import {
  ROLE_DESC,
  useExtractScrub,
  useSaveTemplate,
  type ScrubAction,
  type ScrubFinding,
  type TeamView,
} from '@/api/teams';
import { btnGhost, btnPrimary, Note } from './kit';
import { CheckIcon, WarnIcon } from './teamsUi';

function riskLabel(risk: ScrubFinding['risk'], t: TFunction): string {
  if (risk === 'hi') return t('extractModal.riskHigh');
  if (risk === 'md') return t('extractModal.riskMed');
  return t('extractModal.riskLow');
}

function riskClass(risk: ScrubFinding['risk']): string {
  if (risk === 'hi') return 'text-danger bg-danger/10 border border-danger/30';
  if (risk === 'md') return 'text-warning bg-warning/10 border border-warning/30';
  return 'text-text-muted bg-bg-subtle border border-border-base';
}

export function ExtractModal({
  team,
  onClose,
  onSaved,
}: {
  team: TeamView | null;
  onClose: () => void;
  onSaved: () => void;
}): React.ReactElement | null {
  const { t } = useTranslation('teams');
  const scrub = useExtractScrub(team?.id ?? '');
  const findings = useMemo(() => scrub.data ?? [], [scrub.data]);
  const [actions, setActions] = useState<Record<number, ScrubAction>>({});
  const save = useSaveTemplate();

  // Seed each finding to its default action once the scan resolves.
  useEffect(() => {
    if (findings.length === 0) return;
    setActions((prev) => {
      if (Object.keys(prev).length === findings.length) return prev;
      const next: Record<number, ScrubAction> = {};
      findings.forEach((f, i) => (next[i] = f.default_action));
      return next;
    });
  }, [findings]);

  if (!team) return null;

  // The scan must resolve before the gate can read as passed — otherwise the
  // empty-findings window during loading (hiKept === 0) would flash "已过门" green
  // next to the "扫描中…" spinner and wrongly enable Save.
  const scanning = scrub.isLoading;
  const keptCount = findings.filter((_, i) => actions[i] === 'keep').length;
  const scrubbedCount = findings.length - keptCount;
  const hiKept = findings.filter((f, i) => f.risk === 'hi' && actions[i] === 'keep').length;
  const gatePassed = !scanning && hiKept === 0;

  const set = (i: number, a: ScrubAction) => setActions((prev) => ({ ...prev, [i]: a }));

  const submit = async () => {
    if (!gatePassed || save.isPending) return;
    try {
      await save.mutateAsync({
        name: t('extractModal.templateNameSuffix', { name: team.name }),
        description: t('extractModal.templateDescription', { name: team.name }),
        source: t('extractModal.templateSource', { id: team.id }),
        source_kind: 'extract',
        roles: team.roles.map((r) => ({
          role: r.role,
          cli: r.cli,
          model: r.model,
          capability_tags: r.capability_tags,
          max_concurrency: r.max_concurrency,
          count: r.count ?? 1,
          description: ROLE_DESC[r.role] || '',
        })),
      });
      onClose();
      onSaved();
    } catch {
      /* surfaced via error */
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-auto bg-black/40 p-8 backdrop-blur-sm"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        data-testid="extract-modal"
        className="w-full max-w-3xl rounded-xl border border-border-strong bg-bg-elevated shadow-3"
      >
        <div className="border-b border-border-base px-6 py-5">
          <h2 className="text-base font-semibold text-text-primary">{t('extractModal.title')}</h2>
          <p className="mt-1 text-xs text-text-muted">
            <Trans i18nKey="extractModal.subtitle" ns="teams" values={{ name: team.name }} components={{ b: <b className="font-semibold text-text-primary" /> }} />
          </p>
        </div>

        <div className="max-h-[62vh] overflow-auto px-6 py-5">
          {/* stepper */}
          <div className="mb-5 flex items-center text-xs font-semibold">
            <Step n={<CheckIcon className="h-3 w-3" />} label={t('extractModal.stepSelectSource')} state="done" />
            <span className="mx-3 h-px flex-1 bg-border-base" />
            <Step n="2" label={t('extractModal.stepScrubCurate')} state="active" />
            <span className="mx-3 h-px flex-1 bg-border-base" />
            <Step n="3" label={t('extractModal.stepNameSave')} state="idle" />
          </div>

          <Note>
            <Trans
              i18nKey="extractModal.note"
              ns="teams"
              values={{ count: findings.length }}
              components={{
                b: <b />,
                danger: <span className="font-mono text-danger" />,
                success: <span className="font-mono text-success" />,
              }}
            />
          </Note>

          {scrub.isLoading && <p className="text-sm text-text-muted">{t('extractModal.scanning')}</p>}

          <div data-testid="extract-scrublist">
            {findings.map((f, i) => {
              const kept = actions[i] === 'keep';
              return (
                <div key={i} data-testid={`scrub-${i}`} className="mb-3 overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1">
                  <div className="flex items-center gap-3 px-4 py-3">
                    <span className={`rounded px-2 py-0.5 text-[0.6rem] font-bold uppercase tracking-wide ${riskClass(f.risk)}`}>
                      {riskLabel(f.risk, t)}
                    </span>
                    <span className="min-w-0 flex-1 truncate font-mono text-[0.7rem] text-text-muted">{f.loc}</span>
                    <span className="text-[0.7rem] text-text-muted">{f.reason}</span>
                  </div>
                  <div className="px-4 pb-3 font-mono text-xs leading-relaxed text-text-secondary">
                    <span
                      className={
                        kept
                          ? 'rounded border-b border-dashed border-success bg-success/15 px-1 text-success'
                          : 'rounded border-b border-dashed border-danger bg-danger/10 px-1 text-danger'
                      }
                      data-testid={`scrub-${i}-token`}
                    >
                      {kept ? f.token : '‹placeholder›'}
                    </span>
                  </div>
                  <div className="flex gap-2 border-t border-border-base bg-bg-subtle px-4 py-2.5">
                    <button
                      type="button"
                      data-testid={`scrub-${i}-scrub`}
                      className={[
                        'flex flex-1 items-center justify-center gap-1.5 rounded border px-2 py-1.5 text-xs font-semibold',
                        !kept ? 'border-danger bg-danger/5 text-danger' : 'border-border-base bg-bg-elevated text-text-muted',
                      ].join(' ')}
                      onClick={() => set(i, 'scrub')}
                    >
                      {t('extractModal.removePlaceholder')}
                    </button>
                    <button
                      type="button"
                      data-testid={`scrub-${i}-keep`}
                      className={[
                        'flex flex-1 items-center justify-center gap-1.5 rounded border px-2 py-1.5 text-xs font-semibold',
                        kept ? 'border-success bg-success/15 text-success' : 'border-border-base bg-bg-elevated text-text-muted',
                      ].join(' ')}
                      onClick={() => set(i, 'keep')}
                    >
                      {t('extractModal.keep')}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>

          {/* gate */}
          <div
            data-testid="extract-gate"
            data-gate-state={scanning ? 'scanning' : gatePassed ? 'passed' : 'blocked'}
            className={[
              'mt-1.5 flex items-center gap-3 rounded-lg border px-4 py-3.5',
              scanning
                ? 'border-border-base bg-bg-subtle'
                : gatePassed
                  ? 'border-success/50 bg-success/10'
                  : 'border-warning/40 bg-warning/5',
            ].join(' ')}
          >
            <span className={scanning ? 'text-text-muted' : gatePassed ? 'text-success' : 'text-warning'}>
              {scanning ? (
                <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-border-strong border-t-transparent" aria-hidden="true" />
              ) : gatePassed ? (
                <CheckIcon className="h-5 w-5" />
              ) : (
                <WarnIcon className="h-5 w-5" />
              )}
            </span>
            <div className="text-xs">
              {scanning ? (
                <>
                  <b className="font-semibold text-text-secondary">{t('extractModal.gateScanningTitle')}</b>{' '}
                  <span className="text-text-muted">{t('extractModal.gateScanningBody')}</span>
                </>
              ) : gatePassed ? (
                <>
                  <b className="font-semibold text-success">{t('extractModal.gatePassedTitle')}</b>{' '}
                  <span className="text-text-muted">
                    {t('extractModal.gatePassedBody', { kept: keptCount, scrubbed: scrubbedCount })}
                  </span>
                </>
              ) : (
                <>
                  <b className="font-semibold text-warning">{t('extractModal.gateBlockedTitle')}</b>{' '}
                  <span className="text-text-muted">{t('extractModal.gateBlockedBody', { count: hiKept })}</span>
                </>
              )}
            </div>
          </div>
        </div>

        <div className="flex items-center justify-between gap-3 rounded-b-xl border-t border-border-base bg-bg-subtle px-6 py-4">
          <span className="font-mono text-[0.6875rem] text-text-muted" data-testid="extract-count">
            {t('extractModal.count', { scrubbed: scrubbedCount, kept: keptCount })}
          </span>
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className={btnPrimary}
              disabled={!gatePassed || save.isPending}
              data-testid="extract-save"
              onClick={submit}
            >
              {scanning ? t('extractModal.saveScanning') : !gatePassed ? t('extractModal.saveBlocked') : save.isPending ? t('extractModal.saving') : t('extractModal.save')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function Step({ n, label, state }: { n: React.ReactNode; label: string; state: 'done' | 'active' | 'idle' }): React.ReactElement {
  const circle =
    state === 'done'
      ? 'border-brand bg-brand text-white'
      : state === 'active'
        ? 'border-accent bg-brand/10 text-accent'
        : 'border-border-strong bg-bg-elevated text-text-muted';
  const text = state === 'idle' ? 'text-text-muted' : 'text-text-primary';
  return (
    <span className={`flex items-center gap-2 ${text}`}>
      <span className={`grid h-6 w-6 place-items-center rounded-full border text-[0.65rem] ${circle}`}>{n}</span>
      {label}
    </span>
  );
}
