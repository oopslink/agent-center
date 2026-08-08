// Team WebUI — read-only team-memory two-pane (index tree ↔ rendered doc).
// Team detail owns this as the single product surface for memory entries and
// rules. MEMORY.md is the always-resident index; entries/<slug> and rules/<slug>
// are lazy documents grouped from the index order, without adding a second kind
// field to the DTO.
import { useEffect, useMemo, useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTeamMemoryDoc, useTeamMemoryIndex, type MemoryIndexEntry } from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { ErrorState } from '@/components/ErrorState';
import { Skeleton } from '@/components/Skeleton';
import { btnSm } from './kit';
import { DocIcon, PinIcon } from './teamsUi';

type MemorySection = 'index' | 'entries' | 'rules';
type MemoryFilter = 'all' | 'entries' | 'rules';

interface MemoryNode {
  slug: string;
  pinned: boolean;
  section: MemorySection;
  displayPath: string;
}

export function MemoryPane({ teamId, heading }: { teamId: string; heading: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const index = useTeamMemoryIndex(teamId);
  const nodes = useMemo(() => buildMemoryNodes(index.data ?? []), [index.data]);
  const [filter, setFilter] = useState<MemoryFilter>('all');
  const [slug, setSlug] = useState('');
  const targetNodes = useMemo(
    () => (filter === 'all' ? nodes : nodes.filter((node) => node.section === filter)),
    [filter, nodes],
  );
  const activeNode = targetNodes.find((node) => node.slug === slug) ?? targetNodes[0];
  const doc = useTeamMemoryDoc(teamId, activeNode?.slug ?? '');
  const [raw, setRaw] = useState(false);
  const [copied, setCopied] = useState(false);
  const counts = useMemo(() => ({
    entries: nodes.filter((node) => node.section === 'entries').length,
    rules: nodes.filter((node) => node.section === 'rules').length,
  }), [nodes]);

  useEffect(() => {
    if (!index.isSuccess) return;
    if (!activeNode) {
      if (slug !== '') setSlug('');
      return;
    }
    if (activeNode.slug !== slug) setSlug(activeNode.slug);
  }, [activeNode, index.isSuccess, slug]);

  useEffect(() => {
    setRaw(false);
    setCopied(false);
  }, [activeNode?.slug]);

  const copyPath = async () => {
    if (!doc.data?.path || !navigator.clipboard?.writeText) return;
    try {
      await navigator.clipboard.writeText(doc.data.path);
      setCopied(true);
    } catch {
      // Clipboard permission can be unavailable in embedded browsers/tests.
    }
  };

  return (
    <div
      className="grid min-h-[400px] overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1 md:grid-cols-[260px_1fr]"
      data-testid="memory-pane"
    >
      <div className="border-b border-border-base bg-bg-subtle p-3 md:border-b-0 md:border-r">
        <div className="px-1.5 pb-2 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">{heading}</div>
        <div
          className="mb-3 rounded border border-border-base bg-bg-elevated px-2.5 py-2 text-[0.6875rem] text-text-secondary"
          data-testid="memory-management-capability"
          data-management-available="false"
        >
          {t('memoryPane.permission.readOnly')}
          <button
            type="button"
            className="mt-2 w-full rounded border border-border-base bg-bg-muted px-2 py-1 text-xs font-semibold text-text-muted"
            data-testid="memory-manage"
            disabled
            title={t('memoryPane.permission.manageDisabledTitle')}
          >
            {t('memoryPane.manage')}
          </button>
        </div>
        {index.isLoading && <Skeleton height="8rem" />}
        {index.isError && (
          <ErrorState message={t('memoryPane.error.index')} error={index.error} testId="memory-index-error" />
        )}
        {index.isSuccess && (
          <>
            <div role="tablist" aria-label={t('memoryPane.filter.aria')} className="mb-3 grid grid-cols-3 gap-1 rounded-md border border-border-base bg-bg-elevated p-1">
              {(['all', 'entries', 'rules'] as const).map((key) => (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={filter === key}
                  data-testid={`memory-filter-${key}`}
                  onClick={() => setFilter(key)}
                  className={[
                    'rounded px-2 py-1 text-xs font-semibold motion-safe:transition-colors',
                    filter === key ? 'bg-brand-hover text-white' : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
                  ].join(' ')}
                >
                  {t(`memoryPane.filter.${key}`, { count: key === 'all' ? counts.entries + counts.rules : counts[key] })}
                </button>
              ))}
            </div>
            <div className="font-mono text-xs">
              {filter === 'all' ? (
                <>
                  <MemoryGroup label={t('memoryPane.section.index')} nodes={nodes.filter((node) => node.section === 'index')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  <MemoryGroup label="entries/" nodes={nodes.filter((node) => node.section === 'entries')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  <MemoryGroup label="rules/" nodes={nodes.filter((node) => node.section === 'rules')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  {nodes.length === 0 && <NavEmpty text={t('memoryPane.empty.allTitle')} />}
                </>
              ) : targetNodes.length > 0 ? (
                <MemoryGroup label={`${filter}/`} nodes={targetNodes} activeSlug={activeNode?.slug} onSelect={setSlug} />
              ) : (
                <NavEmpty text={filter === 'rules' ? t('memoryPane.empty.rulesTitle') : t('memoryPane.empty.entriesTitle')} />
              )}
            </div>
          </>
        )}
      </div>

      <div className="overflow-auto p-6" data-testid="memory-view">
        {index.isLoading && <Skeleton height="10rem" />}
        {index.isError && (
          <ErrorState message={t('memoryPane.error.index')} error={index.error} testId="memory-view-index-error" />
        )}
        {index.isSuccess && !activeNode && (
          <EmptyState
            title={filter === 'rules' ? t('memoryPane.empty.rulesTitle') : filter === 'entries' ? t('memoryPane.empty.entriesTitle') : t('memoryPane.empty.allTitle')}
            body={filter === 'rules' ? t('memoryPane.empty.rulesBody') : filter === 'entries' ? t('memoryPane.empty.entriesBody') : t('memoryPane.empty.allBody')}
            testId={filter === 'rules' ? 'memory-rules-empty' : 'memory-empty'}
          />
        )}
        {activeNode && doc.isLoading && <Skeleton height="10rem" />}
        {activeNode && doc.isError && (
          <ErrorState
            message={doc.error instanceof Error && 'status' in doc.error && doc.error.status === 403
              ? t('memoryPane.error.permission')
              : t('memoryPane.error.doc')}
            error={doc.error}
            testId="memory-doc-error"
          />
        )}
        {doc.isSuccess && (
          <>
            <div className="mb-4 flex items-start justify-between gap-3">
              <div>
                <div className="font-mono text-xs text-text-muted">{doc.data.path}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2">
                  <h2 className="text-[1.05rem] font-semibold text-text-primary">{doc.data.title}</h2>
                  {activeNode?.section === 'rules' && <RuleBadge testId="memory-doc-rule-badge" label={t('memoryPane.ruleBadge')} />}
                </div>
              </div>
              <div className="flex gap-2">
                <button type="button" className={btnSm} data-testid="memory-raw-toggle" onClick={() => setRaw((v) => !v)}>
                  {raw ? t('memoryPane.rendered') : t('memoryPane.raw')}
                </button>
                <button type="button" className={btnSm} data-testid="memory-copy-path" onClick={() => void copyPath()}>
                  {copied ? t('memoryPane.copied') : t('memoryPane.copyPath')}
                </button>
              </div>
            </div>
            {raw ? (
              <pre className="whitespace-pre-wrap rounded-lg border border-border-base bg-bg-subtle p-3.5 font-mono text-xs text-text-secondary" data-testid="memory-raw-view">
                {rawDoc(doc.data.frontmatter, doc.data.body)}
              </pre>
            ) : (
              <>
                {doc.data.frontmatter && (
                  <pre className="mb-4 whitespace-pre-wrap rounded-lg border border-border-base bg-bg-subtle p-3.5 font-mono text-[0.7rem] text-text-muted">
                    {doc.data.frontmatter}
                  </pre>
                )}
                <div className="prose-team text-sm leading-relaxed text-text-secondary [&_code]:rounded [&_code]:bg-brand/10 [&_code]:px-1.5 [&_code]:py-px [&_code]:font-mono [&_code]:text-[0.75rem] [&_code]:text-brand-hover [&_h4]:mb-2 [&_h4]:mt-4 [&_h4]:text-[0.7rem] [&_h4]:font-semibold [&_h4]:uppercase [&_h4]:tracking-wide [&_h4]:text-brand-hover [&_li]:my-1 [&_p]:mb-2.5 [&_ul]:ml-4 [&_ul]:list-disc [&_ul]:text-text-muted">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{doc.data.body}</ReactMarkdown>
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function MemoryGroup({
  label,
  nodes,
  activeSlug,
  onSelect,
}: {
  label: string;
  nodes: MemoryNode[];
  activeSlug?: string;
  onSelect: (slug: string) => void;
}): React.ReactElement | null {
  const { t } = useTranslation('teams');
  if (nodes.length === 0) return null;
  return (
    <div data-testid={`memory-section-${label.replace(/[^a-z0-9]+/gi, '').toLowerCase()}`}>
      <div className="px-2 pb-1 pt-3 font-sans text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
        {label}
      </div>
      {nodes.map((node) => {
        const active = node.slug === activeSlug;
        return (
          <div key={`${node.section}-${node.slug}`} className={node.pinned ? '' : 'ml-3.5 border-l border-border-base'}>
            <button
              type="button"
              data-testid={`memory-node-${node.slug}`}
              aria-current={active ? 'true' : undefined}
              className={[
                'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left motion-safe:transition-colors',
                active ? 'bg-brand/10 text-brand-hover' : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
              ].join(' ')}
              onClick={() => onSelect(node.slug)}
            >
              <span className={node.pinned ? 'text-brand' : 'opacity-70'} aria-hidden="true">
                {node.pinned ? <PinIcon className="h-3.5 w-3.5" /> : <DocIcon className="h-3.5 w-3.5" />}
              </span>
              <span className="min-w-0 flex-1 truncate">{node.displayPath}</span>
              {node.section === 'rules' && <RuleBadge testId={`memory-rule-badge-${node.slug}`} label={t('memoryPane.ruleBadge')} />}
              <span
                className={[
                  'rounded px-1.5 py-px font-sans text-[0.55rem] font-semibold',
                  node.pinned ? 'border border-brand/25 text-brand' : 'border border-border-base text-text-muted',
                ].join(' ')}
              >
                {node.pinned ? t('memoryPane.pinned') : t('memoryPane.lazy')}
              </span>
            </button>
          </div>
        );
      })}
    </div>
  );
}

function RuleBadge({ label, testId }: { label: string; testId?: string }): React.ReactElement {
  return (
    <span
      data-testid={testId}
      className="shrink-0 rounded border border-warning/40 bg-warning/10 px-1.5 py-px font-mono text-[0.55rem] font-semibold uppercase text-warning"
    >
      {label}
    </span>
  );
}

function NavEmpty({ text }: { text: string }): React.ReactElement {
  return (
    <div className="rounded border border-dashed border-border-base bg-bg-elevated px-3 py-4 text-center font-sans text-xs text-text-muted" data-testid="memory-nav-empty">
      {text}
    </div>
  );
}

function buildMemoryNodes(entries: MemoryIndexEntry[]): MemoryNode[] {
  let currentSection: Exclude<MemorySection, 'index'> = 'entries';
  const nodes: MemoryNode[] = [];
  for (const entry of entries) {
    if (entry.group) {
      currentSection = sectionFromGroup(entry.group);
      continue;
    }
    if (!entry.slug) continue;
    const pinned = entry.pinned === true || entry.slug === 'MEMORY.md';
    const section = pinned ? 'index' : sectionFromSlug(entry.slug, currentSection);
    nodes.push({
      slug: entry.slug,
      pinned,
      section,
      displayPath: displayPath(entry.slug, section),
    });
  }
  return nodes;
}

function sectionFromGroup(group: string): Exclude<MemorySection, 'index'> {
  return group.trim().toLowerCase().startsWith('rules') ? 'rules' : 'entries';
}

function sectionFromSlug(slug: string, fallback: Exclude<MemorySection, 'index'>): Exclude<MemorySection, 'index'> {
  const lower = slug.toLowerCase();
  if (lower.startsWith('rules/')) return 'rules';
  if (lower.startsWith('entries/')) return 'entries';
  return fallback;
}

function displayPath(slug: string, section: MemorySection): string {
  if (section === 'index') return slug;
  const stripped = slug.replace(/^(entries|rules)\//, '').replace(/\.md$/, '');
  return `${section}/${stripped}.md`;
}

function rawDoc(frontmatter: string | null, body: string): string {
  if (!frontmatter) return body;
  return `---\n${frontmatter}\n---\n\n${body}`;
}
