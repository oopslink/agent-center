// Team WebUI — read-only team-memory two-pane (index tree ↔ rendered doc).
// Team detail owns this as the single product surface for memory entries and
// rules. MEMORY.md is the always-resident index; entries/<slug> and rules/<slug>
// are lazy documents grouped from the index order, without adding a second kind
// field to the DTO.
import { useEffect, useMemo, useState } from 'react';
import type React from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  useCreateTeamMemoryProposal,
  usePromoteTeamMemoryProposal,
  useRejectTeamMemoryProposal,
  useTeamMemoryDoc,
  useTeamMemoryIndex,
  type MemoryIndexEntry,
  type TeamMemoryProposal,
  type TeamView,
} from '@/api/teams';
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import { EmptyState } from '@/components/EmptyState';
import { ErrorState } from '@/components/ErrorState';
import { Skeleton } from '@/components/Skeleton';
import { ToggleSwitch } from '@/components/ToggleSwitch';
import { btnGhost, btnSm, btnSmDanger, btnSmPrimary, Field, inputCls, ModalShell, Note } from './kit';
import { DocIcon, PinIcon } from './teamsUi';

type MemorySection = 'index' | 'entries' | 'rules' | 'proposals';
type MemoryFilter = 'all' | 'entries' | 'rules' | 'proposals';

interface MemoryNode {
  slug: string;
  pinned: boolean;
  section: MemorySection;
  displayPath: string;
  kind: MemoryIndexEntry['kind'];
  status?: MemoryIndexEntry['status'];
  targetKind?: MemoryIndexEntry['target_kind'];
  sourcePath?: string;
  uuid?: string;
  commit?: string;
}

type PermissionState = 'manage' | 'read-only' | 'unavailable';
const phaseOptions = ['plan', 'execute', 'review', 'recovery'].map((phase) => ({ value: phase, label: phase }));

export function MemoryPane({
  teamId,
  heading,
  team,
  currentUserRole,
}: {
  teamId: string;
  heading: string;
  team?: TeamView;
  currentUserRole?: string;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const index = useTeamMemoryIndex(teamId);
  const nodes = useMemo(() => buildMemoryNodes(index.data ?? []), [index.data]);
  const permission = memoryPermission(team, currentUserRole);
  const [filter, setFilter] = useState<MemoryFilter>('all');
  const [slug, setSlug] = useState('');
  const [creating, setCreating] = useState(false);
  const targetNodes = useMemo(
    () => (filter === 'all' ? nodes : nodes.filter((node) => node.section === filter)),
    [filter, nodes],
  );
  const activeNode = targetNodes.find((node) => node.slug === slug) ?? targetNodes[0];
  const doc = useTeamMemoryDoc(teamId, activeNode?.slug ?? '', activeNode?.kind);
  const [raw, setRaw] = useState(false);
  const [copied, setCopied] = useState(false);
  const counts = useMemo(() => ({
    entries: nodes.filter((node) => node.section === 'entries').length,
    rules: nodes.filter((node) => node.section === 'rules').length,
    proposals: nodes.filter((node) => node.section === 'proposals').length,
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
          data-testid="memory-permission"
          data-can-manage={permission.canManage == null ? 'unavailable' : permission.canManage ? 'true' : 'false'}
          data-permission-state={permission.state}
        >
          {permissionText(permission.state, permission.role, t)}
          <button
            type="button"
            className="mt-2 w-full rounded border border-border-base bg-bg-muted px-2 py-1 text-xs font-semibold text-text-muted"
            data-testid="memory-manage"
            disabled={permission.canManage !== true}
            title={permissionTitle(permission.state, t)}
            onClick={() => setCreating(true)}
          >
            {t('memoryPane.newProposal')}
          </button>
        </div>
        <Note testId="memory-effect-hint">{t('memoryPane.effectHint')}</Note>
        {index.isLoading && <Skeleton height="8rem" />}
        {index.isError && (
          <ErrorState message={t('memoryPane.error.index')} error={index.error} testId="memory-index-error" />
        )}
        {index.isSuccess && (
          <>
            <div role="tablist" aria-label={t('memoryPane.filter.aria')} className="mb-3 grid grid-cols-2 gap-1 rounded-md border border-border-base bg-bg-elevated p-1 sm:grid-cols-4">
              {(['all', 'entries', 'rules', 'proposals'] as const).map((key) => (
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
                  {t(`memoryPane.filter.${key}`, { count: key === 'all' ? counts.entries + counts.rules + counts.proposals : counts[key] })}
                </button>
              ))}
            </div>
            <div className="font-mono text-xs">
              {filter === 'all' ? (
                <>
                  <MemoryGroup label={t('memoryPane.section.index')} nodes={nodes.filter((node) => node.section === 'index')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  <MemoryGroup label="entries/" nodes={nodes.filter((node) => node.section === 'entries')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  <MemoryGroup label="rules/" nodes={nodes.filter((node) => node.section === 'rules')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  <MemoryGroup label="proposals/" nodes={nodes.filter((node) => node.section === 'proposals')} activeSlug={activeNode?.slug} onSelect={setSlug} />
                  {nodes.length === 0 && <NavEmpty text={t('memoryPane.empty.allTitle')} />}
                </>
              ) : targetNodes.length > 0 ? (
                <MemoryGroup label={`${filter}/`} nodes={targetNodes} activeSlug={activeNode?.slug} onSelect={setSlug} />
              ) : (
                <NavEmpty text={filter === 'rules' ? t('memoryPane.empty.rulesTitle') : filter === 'proposals' ? t('memoryPane.empty.proposalsTitle') : t('memoryPane.empty.entriesTitle')} />
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
            title={filter === 'rules' ? t('memoryPane.empty.rulesTitle') : filter === 'proposals' ? t('memoryPane.empty.proposalsTitle') : filter === 'entries' ? t('memoryPane.empty.entriesTitle') : t('memoryPane.empty.allTitle')}
            body={filter === 'rules' ? t('memoryPane.empty.rulesBody') : filter === 'proposals' ? t('memoryPane.empty.proposalsBody') : filter === 'entries' ? t('memoryPane.empty.entriesBody') : t('memoryPane.empty.allBody')}
            testId={filter === 'rules' ? 'memory-rules-empty' : filter === 'proposals' ? 'memory-proposals-empty' : 'memory-empty'}
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
                <div className="font-mono text-xs text-text-muted">{doc.data.source_path ?? doc.data.path}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2">
                  <h2 className="text-[1.05rem] font-semibold text-text-primary">{doc.data.title}</h2>
                  {activeNode?.section === 'rules' && <RuleBadge testId="memory-doc-rule-badge" label={t('memoryPane.ruleBadge')} />}
                  {activeNode?.section === 'proposals' && <ProposalBadge status={doc.data.proposal?.status ?? activeNode.status} />}
                </div>
                <MemoryMeta sourcePath={doc.data.source_path ?? doc.data.path} uuid={doc.data.uuid} commit={doc.data.commit} />
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
            {doc.data.proposal ? (
              <ProposalDetail
                teamId={teamId}
                proposal={doc.data.proposal}
                canManage={permission.canManage === true}
              />
            ) : raw ? (
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
                  <ReactMarkdown remarkPlugins={[remarkGfm]} components={safeMarkdownComponents}>
                    {doc.data.body}
                  </ReactMarkdown>
                </div>
              </>
            )}
          </>
        )}
      </div>
      {creating && (
        <CreateProposalModal teamId={teamId} onClose={() => setCreating(false)} />
      )}
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
              {node.section === 'proposals' && <ProposalBadge status={node.status} testId={`memory-proposal-badge-${node.slug}`} />}
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

function ProposalBadge({ status, testId }: { status?: string; testId?: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const label = status ? t(`memoryPane.proposalStatus.${status}`, { defaultValue: status }) : t('memoryPane.proposalStatus.pending');
  const tone =
    status === 'promoted'
      ? 'border-success/40 bg-success/10 text-success'
      : status === 'rejected'
        ? 'border-danger/40 bg-danger/10 text-danger'
        : 'border-accent/40 bg-accent/10 text-accent';
  return (
    <span data-testid={testId} className={`shrink-0 rounded border px-1.5 py-px font-mono text-[0.55rem] font-semibold uppercase ${tone}`}>
      {label}
    </span>
  );
}

function MemoryMeta({ sourcePath, uuid, commit }: { sourcePath?: string; uuid?: string; commit?: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  return (
    <dl className="mt-2 grid gap-1 text-[0.68rem] text-text-muted sm:grid-cols-3" data-testid="memory-doc-meta">
      <div>
        <dt className="font-semibold uppercase">{t('memoryPane.meta.sourcePath')}</dt>
        <dd className="font-mono break-all">{sourcePath || '—'}</dd>
      </div>
      <div>
        <dt className="font-semibold uppercase">{t('memoryPane.meta.uuid')}</dt>
        <dd className="font-mono break-all">{uuid || '—'}</dd>
      </div>
      <div>
        <dt className="font-semibold uppercase">{t('memoryPane.meta.commit')}</dt>
        <dd className="font-mono break-all">{commit ? commit.slice(0, 12) : '—'}</dd>
      </div>
    </dl>
  );
}

function ProposalDetail({
  teamId,
  proposal,
  canManage,
}: {
  teamId: string;
  proposal: TeamMemoryProposal;
  canManage: boolean;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const promote = usePromoteTeamMemoryProposal(teamId);
  const reject = useRejectTeamMemoryProposal(teamId);
  const [reason, setReason] = useState('');
  const pending = proposal.status === 'pending';
  return (
    <div className="space-y-4" data-testid="memory-proposal-detail">
      <div className="grid gap-2 rounded border border-border-base bg-bg-subtle p-3 text-xs text-text-secondary sm:grid-cols-2">
        <Spec label={t('memoryPane.proposal.target')} value={`${proposal.target_kind}/${proposal.slug}`} />
        <Spec label={t('memoryPane.proposal.author')} value={proposal.author_ref || '—'} />
        <Spec label={t('memoryPane.proposal.ack')} value={proposal.warning_acknowledged ? t('memoryPane.proposal.yes') : t('memoryPane.proposal.no')} />
        <Spec label={t('memoryPane.proposal.created')} value={proposal.created_at || '—'} />
        {proposal.promoted_path && <Spec label={t('memoryPane.proposal.promotedPath')} value={proposal.promoted_path} />}
        {proposal.reject_reason && <Spec label={t('memoryPane.proposal.rejectReason')} value={proposal.reject_reason} />}
      </div>
      <div className="prose-team text-sm leading-relaxed text-text-secondary">
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={safeMarkdownComponents}>
          {proposal.body}
        </ReactMarkdown>
      </div>
      <div>
        <h3 className="mb-2 text-xs font-semibold uppercase text-text-muted">{t('memoryPane.proposal.diff')}</h3>
        <pre className="max-h-80 overflow-auto rounded border border-border-base bg-bg-subtle p-3 font-mono text-xs text-text-secondary" data-testid="memory-proposal-diff">
          {proposal.diff}
        </pre>
      </div>
      {pending && (
        <div className="rounded border border-border-base bg-bg-elevated p-3" data-testid="memory-proposal-actions">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <button
              type="button"
              className={btnSmPrimary}
              disabled={!canManage || promote.isPending}
			  onClick={() => promote.mutate({ proposal_id: proposal.id, expected_repo_commit: proposal.commit, comment: reason, acknowledge_warnings: proposal.warnings })}
              data-testid="memory-proposal-promote"
            >
              {t('memoryPane.proposal.promote')}
            </button>
            <button
              type="button"
              className={btnSmDanger}
              disabled={!canManage || reject.isPending}
			  onClick={() => reject.mutate({ proposal_id: proposal.id, expected_repo_commit: proposal.commit, comment: reason })}
              data-testid="memory-proposal-reject"
            >
              {t('memoryPane.proposal.reject')}
            </button>
            {!canManage && <span className="text-xs text-text-muted">{t('memoryPane.proposal.readOnly')}</span>}
          </div>
          <input
            className={inputCls}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={t('memoryPane.proposal.rejectPlaceholder')}
            data-testid="memory-proposal-reject-reason"
          />
          {(promote.isError || reject.isError) && (
            <p className="mt-2 text-xs text-danger" role="alert">
              {((promote.error || reject.error) as Error).message}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function Spec({ label, value }: { label: string; value: string }): React.ReactElement {
  return (
    <div>
      <div className="text-[0.62rem] font-semibold uppercase text-text-muted">{label}</div>
      <div className="font-mono break-all">{value}</div>
    </div>
  );
}

function CreateProposalModal({ teamId, onClose }: { teamId: string; onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('teams');
  const create = useCreateTeamMemoryProposal();
  const [targetKind, setTargetKind] = useState<'entry' | 'rule'>('entry');
  const [slug, setSlug] = useState('');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [body, setBody] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [appliesTo, setAppliesTo] = useState(['execute']);
  const [ack, setAck] = useState(false);
  const invalid = !slug.trim() || !description.trim() || !body.trim() || !ack;
  const submit = async () => {
    if (invalid) return;
    await create.mutateAsync({
      team_id: teamId,
      target_kind: targetKind,
      slug: slug.trim(),
      title: title.trim(),
      description: description.trim(),
      body,
      enabled,
      applies_to: targetKind === 'rule' ? appliesTo : [],
      warning_acknowledged: ack,
    });
    onClose();
  };
  return (
    <ModalShell
      open
      onClose={onClose}
      wide
      testId="memory-create-proposal-modal"
      title={t('memoryPane.create.title')}
      subtitle={t('memoryPane.create.subtitle')}
      footer={
        <>
          <span />
          <div className="flex gap-2">
            <button type="button" className={btnGhost} onClick={onClose}>{t('common.cancel')}</button>
            <button type="button" className={btnSmPrimary} disabled={invalid || create.isPending} onClick={() => void submit()} data-testid="memory-create-proposal-submit">
              {t('memoryPane.create.submit')}
            </button>
          </div>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t('memoryPane.create.kind')} required>
          <select className={inputCls} value={targetKind} onChange={(e) => setTargetKind(e.target.value as 'entry' | 'rule')} data-testid="memory-create-kind">
            <option value="entry">{t('memoryPane.create.entry')}</option>
            <option value="rule">{t('memoryPane.create.rule')}</option>
          </select>
        </Field>
        <Field label={t('memoryPane.create.slug')} required>
          <input className={inputCls} value={slug} onChange={(e) => setSlug(e.target.value)} data-testid="memory-create-slug" />
        </Field>
      </div>
      <Field label={t('memoryPane.create.titleLabel')}>
        <input className={inputCls} value={title} onChange={(e) => setTitle(e.target.value)} data-testid="memory-create-title" />
      </Field>
      <Field label={t('memoryPane.create.description')} required>
        <input className={inputCls} value={description} onChange={(e) => setDescription(e.target.value)} data-testid="memory-create-description" />
      </Field>
      {targetKind === 'rule' && (
        <div className="mb-4 grid gap-3 rounded border border-border-base bg-bg-subtle p-3 sm:grid-cols-[minmax(0,12rem)_1fr]">
          <div className="flex items-center justify-between gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2">
            <span className="text-sm text-text-secondary">{t('memoryPane.create.enabled')}</span>
            <ToggleSwitch checked={enabled} onChange={setEnabled} ariaLabel={t('memoryPane.create.enabled')} testId="memory-create-enabled" />
          </div>
          <Field label={t('memoryPane.create.appliesTo')}>
            <EntityMultiSelect
              testId="memory-create-applies-to"
              options={phaseOptions}
              values={appliesTo}
              onChange={setAppliesTo}
              ariaLabel={t('memoryPane.create.appliesTo')}
              placeholder={t('memoryPane.create.appliesTo')}
            />
          </Field>
        </div>
      )}
      <Field label={t('memoryPane.create.body')} required>
        <textarea className={`${inputCls} min-h-40 font-mono`} value={body} onChange={(e) => setBody(e.target.value)} data-testid="memory-create-body" />
      </Field>
      <div className="flex items-start gap-2 rounded border border-warning/40 bg-warning/10 p-3 text-xs text-text-secondary">
        <button
          type="button"
          role="checkbox"
          aria-checked={ack}
          aria-label={t('memoryPane.create.warningAck')}
          onClick={() => setAck((v) => !v)}
          data-testid="memory-create-ack"
          className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border ${ack ? 'border-warning bg-warning text-bg-base' : 'border-warning/60 bg-bg-elevated'}`}
        >
          {ack && (
            <svg aria-hidden="true" viewBox="0 0 16 16" className="h-3 w-3 stroke-current" fill="none" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3.5 8.5 6.5 11.5 12.5 4.5" />
            </svg>
          )}
        </button>
        <span>{t('memoryPane.create.warningAck')}</span>
      </div>
      {create.isError && <p className="mt-3 text-xs text-danger" role="alert">{(create.error as Error).message}</p>}
    </ModalShell>
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
    const section = pinned ? 'index' : sectionFromEntry(entry, currentSection);
    nodes.push({
      slug: entry.slug,
      pinned,
      section,
      displayPath: displayPath(entry.slug, section),
      kind: entry.kind ?? kindFromSection(section),
      status: entry.status,
      targetKind: entry.target_kind,
      sourcePath: entry.source_path ?? entry.path,
      uuid: entry.uuid,
      commit: entry.commit,
    });
  }
  return nodes;
}

function sectionFromGroup(group: string): Exclude<MemorySection, 'index'> {
  const normalized = normalizeGroup(group);
  if (normalized === 'rules') return 'rules';
  if (normalized === 'proposals') return 'proposals';
  return 'entries';
}

function sectionFromEntry(entry: MemoryIndexEntry, fallback: Exclude<MemorySection, 'index'>): Exclude<MemorySection, 'index'> {
  if (entry.kind === 'proposal') return 'proposals';
  if (entry.kind === 'rule') return 'rules';
  if (entry.kind === 'entry') return 'entries';
  if (hasSectionPrefix(entry.slug, 'proposals') || hasSectionPrefix(entry.path, 'proposals')) return 'proposals';
  if (hasSectionPrefix(entry.slug, 'rules') || hasSectionPrefix(entry.path, 'rules')) return 'rules';
  if (hasSectionPrefix(entry.slug, 'entries') || hasSectionPrefix(entry.path, 'entries')) return 'entries';
  return fallback;
}

function normalizeGroup(group: string): string {
  return group.trim().toLowerCase().replace(/\/+$/, '');
}

function hasSectionPrefix(value: string | undefined, section: Exclude<MemorySection, 'index'>): boolean {
  if (!value) return false;
  const normalized = value.trim().replace(/^\.\/+/, '').toLowerCase();
  const path = normalized.startsWith('team-memory/') ? normalized.slice('team-memory/'.length) : normalized;
  return path.startsWith(`${section}/`);
}

function displayPath(slug: string, section: MemorySection): string {
  if (section === 'index') return slug;
  if (section === 'proposals') return `proposals/${slug}.md`;
  const stripped = slug.replace(/^(entries|rules)\//, '').replace(/\.md$/, '');
  return `${section}/${stripped}.md`;
}

function kindFromSection(section: MemorySection): MemoryIndexEntry['kind'] {
  if (section === 'rules') return 'rule';
  if (section === 'proposals') return 'proposal';
  if (section === 'index') return 'index';
  return 'entry';
}

const safeMarkdownComponents = {
  a({ href, children }: { href?: string; children?: React.ReactNode }) {
    const safe = safeUrl(href, true);
    return (
      <a href={safe} rel="noopener noreferrer" target="_blank" className="text-accent underline">
        {children}
      </a>
    );
  },
  img({ src, alt }: { src?: string; alt?: string }) {
    const safe = safeUrl(src, false);
    if (!safe) return null;
    return <img src={safe} alt={alt ?? ''} loading="lazy" className="max-w-full rounded" />;
  },
};

function safeUrl(raw: string | undefined, allowRelative: boolean): string | undefined {
  if (!raw) return undefined;
  const trimmed = raw.trim();
  if (allowRelative && (trimmed.startsWith('/') || trimmed.startsWith('#') || trimmed.startsWith('./') || trimmed.startsWith('../'))) {
    return trimmed;
  }
  try {
    const base = typeof window !== 'undefined' ? window.location.origin : 'http://localhost';
    const url = new URL(trimmed, base);
    if (url.protocol === 'http:' || url.protocol === 'https:' || (allowRelative && url.protocol === 'mailto:')) {
      return trimmed;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

function rawDoc(frontmatter: string | null, body: string): string {
  if (!frontmatter) return body;
  return `---\n${frontmatter}\n---\n\n${body}`;
}

function memoryPermission(team: TeamView | undefined, currentUserRole: string | undefined): {
  state: PermissionState;
  canManage: boolean | null;
  role: string | undefined;
} {
  const permissions = team?.memory_permissions;
  const role = currentUserRole;
  if (permissions?.can_manage != null) {
    return { state: permissions.can_manage ? 'manage' : 'read-only', canManage: permissions.can_manage, role };
  }
  if (permissions?.web_edit !== true) {
    return { state: 'unavailable', canManage: null, role };
  }
  const canManage = role === 'owner' || role === 'admin';
  return { state: canManage ? 'manage' : 'read-only', canManage, role };
}

function permissionText(
  state: PermissionState,
  role: string | undefined,
  t: TFunction<'teams'>,
): string {
  if (state === 'unavailable') return t('memoryPane.permission.unavailable');
  const roleLabel = role ?? t('memoryPane.permission.unknownRole');
  return state === 'manage'
    ? t('memoryPane.permission.manage', { role: roleLabel })
    : t('memoryPane.permission.readOnly', { role: roleLabel });
}

function permissionTitle(state: PermissionState, t: TFunction<'teams'>): string {
  if (state === 'manage') return t('memoryPane.permission.manageTitle');
  if (state === 'read-only') return t('memoryPane.permission.readOnlyTitle');
  return t('memoryPane.permission.unavailableTitle');
}
