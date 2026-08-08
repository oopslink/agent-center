// Team WebUI — team-memory two-pane (index tree ↔ rendered doc).
// Team detail is the single product surface for memory entries/ and rules/.
// MEMORY.md is the always-resident index; entries/ and rules/ docs are lazy.
import { useEffect, useMemo, useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ApiError } from '@/api/client';
import { useTeamMemoryDoc, useTeamMemoryIndex, type MemoryIndexEntry } from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { btnSm } from './kit';
import { DocIcon, PinIcon } from './teamsUi';

type MemoryFilter = 'all' | 'entries' | 'rules';
type MemoryBucket = 'index' | 'entries' | 'rules' | 'other';

function bucketForSlug(slug: string): MemoryBucket {
  if (slug === 'MEMORY.md') return 'index';
  if (slug.startsWith('rules/')) return 'rules';
  if (slug.startsWith('entries/')) return 'entries';
  return 'entries';
}

function groupBucket(group: string): MemoryBucket {
  if (group.startsWith('rules')) return 'rules';
  if (group.startsWith('entries')) return 'entries';
  return 'other';
}

function itemNodes(nodes: readonly MemoryIndexEntry[]): MemoryIndexEntry[] {
  return nodes.filter((node) => !!node.slug);
}

function errorCopy(error: unknown, permissionCopy: string): string {
  if (error instanceof ApiError && error.status === 403) return permissionCopy;
  return error instanceof Error ? error.message : String(error);
}

export function MemoryPane({ teamId, heading }: { teamId: string; heading: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const index = useTeamMemoryIndex(teamId);
  const [slug, setSlug] = useState('');
  const [filter, setFilter] = useState<MemoryFilter>('all');
  const [rawMode, setRawMode] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const nodes = useMemo(() => itemNodes(index.data ?? []), [index.data]);
  const counts = useMemo(
    () => ({
      entries: nodes.filter((node) => bucketForSlug(node.slug!) === 'entries').length,
      rules: nodes.filter((node) => bucketForSlug(node.slug!) === 'rules').length,
    }),
    [nodes],
  );
  const visibleNodes = useMemo(
    () => nodes.filter((node) => filter === 'all' || bucketForSlug(node.slug!) === filter),
    [filter, nodes],
  );

  useEffect(() => {
    if (!index.isSuccess) return;
    const next = visibleNodes[0]?.slug ?? '';
    if (!visibleNodes.some((node) => node.slug === slug)) setSlug(next);
  }, [index.isSuccess, slug, visibleNodes]);

  const doc = useTeamMemoryDoc(teamId, slug);
  const permissionDenied = t('memoryPane.permissionDenied');

  const filterButton = (key: MemoryFilter, label: string, count?: number) => (
    <button
      type="button"
      data-testid={`memory-filter-${key}`}
      aria-pressed={filter === key}
      className={[
        'rounded px-2 py-1 text-[0.65rem] font-semibold motion-safe:transition-colors',
        filter === key ? 'bg-brand-hover text-white' : 'border border-border-base bg-bg-elevated text-text-secondary hover:bg-bg-subtle',
      ].join(' ')}
      onClick={() => setFilter(key)}
    >
      {label}
      {count != null && <span className="ml-1 font-mono opacity-75">{count}</span>}
    </button>
  );

  return (
    <div
      className="grid min-h-[400px] overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1 md:grid-cols-[260px_1fr]"
      data-testid="memory-pane"
    >
      <div className="border-b border-border-base bg-bg-subtle p-3 md:border-b-0 md:border-r">
        <div className="px-1.5 pb-2.5 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">{heading}</div>
        <div className="mb-3 flex flex-wrap gap-1.5" role="group" aria-label={t('memoryPane.filterAria')}>
          {filterButton('all', t('memoryPane.filterAll'))}
          {filterButton('entries', t('memoryPane.filterEntries'), counts.entries)}
          {filterButton('rules', t('memoryPane.filterRules'), counts.rules)}
        </div>
        <button
          type="button"
          className={[btnSm, 'mb-3 w-full justify-center'].join(' ')}
          data-testid="memory-manage-rules"
          onClick={() => setStatus(t('memoryPane.permissionFeedback'))}
        >
          {t('memoryPane.manageRules')}
        </button>
        {status && (
          <p className="mb-3 rounded border border-border-base bg-bg-elevated px-2.5 py-2 text-xs text-text-secondary" data-testid="memory-permission-feedback" role="status">
            {status}
          </p>
        )}
        {index.isLoading && <Skeleton height="8rem" />}
        {index.isError && (
          <p className="rounded border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger" data-testid="memory-index-error" role="alert">
            {errorCopy(index.error, permissionDenied)}
          </p>
        )}
        {index.isSuccess && visibleNodes.length === 0 && (
          <EmptyState
            title={filter === 'rules' ? t('memoryPane.emptyRulesTitle') : t('memoryPane.emptyTitle')}
            body={filter === 'rules' ? t('memoryPane.emptyRulesBody') : t('memoryPane.emptyBody')}
            testId={filter === 'rules' ? 'memory-rules-empty' : 'memory-empty'}
          />
        )}
        <div className="font-mono text-xs">
          {(index.data ?? []).map((node, i) => {
            if (node.group) {
              const group = groupBucket(node.group);
              if (filter !== 'all' && group !== filter) return null;
              return (
                <div key={`g-${i}`} className="px-2 pb-1 pt-3 font-sans text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
                  {node.group}
                </div>
              );
            }
            if (!node.slug) return null;
            const bucket = bucketForSlug(node.slug);
            if (filter !== 'all' && bucket !== filter) return null;
            const isRule = bucket === 'rules';
            const active = node.slug === slug;
            return (
              <div key={node.slug} className={node.pinned ? '' : 'ml-3.5 border-l border-border-base'}>
                <button
                  type="button"
                  data-testid={`memory-node-${node.slug}`}
                  aria-current={active ? 'true' : undefined}
                  className={[
                    'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left motion-safe:transition-colors',
                    active ? 'bg-brand/10 text-brand-hover' : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
                  ].join(' ')}
                  onClick={() => setSlug(node.slug!)}
                >
                  <span className={node.pinned ? 'text-brand' : 'opacity-70'} aria-hidden="true">
                    {node.pinned ? <PinIcon className="h-3.5 w-3.5" /> : <DocIcon className="h-3.5 w-3.5" />}
                  </span>
                  <span className="truncate">{node.slug}</span>
                  <span
                    data-testid={isRule ? `memory-rule-badge-${node.slug}` : undefined}
                    className={[
                      'ml-auto rounded px-1.5 py-px font-sans text-[0.55rem] font-semibold',
                      isRule
                        ? 'border border-warning/40 bg-warning/10 text-warning'
                        : node.pinned
                          ? 'border border-brand/25 text-brand'
                          : 'border border-border-base text-text-muted',
                    ].join(' ')}
                  >
                    {isRule ? t('memoryPane.rule') : node.pinned ? t('memoryPane.pinned') : t('memoryPane.lazy')}
                  </span>
                </button>
              </div>
            );
          })}
        </div>
      </div>

      <div className="overflow-auto p-6" data-testid="memory-view">
        {(index.isLoading || doc.isLoading) && <Skeleton height="10rem" />}
        {index.isError && (
          <p className="rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" data-testid="memory-view-error" role="alert">
            {errorCopy(index.error, permissionDenied)}
          </p>
        )}
        {doc.isError && (
          <p className="rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" data-testid="memory-doc-error" role="alert">
            {errorCopy(doc.error, permissionDenied)}
          </p>
        )}
        {doc.isSuccess && (
          <>
            <div className="mb-4 flex items-start justify-between gap-3">
              <div>
                <div className="font-mono text-xs text-text-muted">{doc.data.path}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2">
                  <h2 className="text-[1.05rem] font-semibold text-text-primary">{doc.data.title}</h2>
                  {bucketForSlug(doc.data.slug) === 'rules' && (
                    <span className="rounded border border-warning/40 bg-warning/10 px-2 py-0.5 text-[0.6rem] font-semibold uppercase tracking-wide text-warning" data-testid="memory-rule-doc-badge">
                      {t('memoryPane.rule')}
                    </span>
                  )}
                </div>
              </div>
              <div className="flex gap-2">
                <button type="button" className={btnSm} data-testid="memory-raw-toggle" aria-pressed={rawMode} onClick={() => setRawMode((v) => !v)}>
                  {rawMode ? t('memoryPane.rendered') : t('memoryPane.raw')}
                </button>
                <button
                  type="button"
                  className={btnSm}
                  onClick={() => {
                    void navigator.clipboard?.writeText(doc.data.path);
                    setStatus(t('memoryPane.copiedPath'));
                  }}
                >
                  {t('memoryPane.copyPath')}
                </button>
              </div>
            </div>
            {doc.data.frontmatter && (
              <pre className="mb-4 whitespace-pre-wrap rounded-lg border border-border-base bg-bg-subtle p-3.5 font-mono text-[0.7rem] text-text-muted">
                {doc.data.frontmatter}
              </pre>
            )}
            {rawMode ? (
              <pre className="whitespace-pre-wrap rounded-lg border border-border-base bg-bg-subtle p-3.5 font-mono text-xs leading-relaxed text-text-secondary" data-testid="memory-raw-body">
                {doc.data.body}
              </pre>
            ) : (
              <div className="prose-team text-sm leading-relaxed text-text-secondary [&_code]:rounded [&_code]:bg-brand/10 [&_code]:px-1.5 [&_code]:py-px [&_code]:font-mono [&_code]:text-[0.75rem] [&_code]:text-brand-hover [&_h4]:mb-2 [&_h4]:mt-4 [&_h4]:text-[0.7rem] [&_h4]:font-semibold [&_h4]:uppercase [&_h4]:tracking-wide [&_h4]:text-brand-hover [&_li]:my-1 [&_p]:mb-2.5 [&_ul]:ml-4 [&_ul]:list-disc [&_ul]:text-text-muted">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{doc.data.body}</ReactMarkdown>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
