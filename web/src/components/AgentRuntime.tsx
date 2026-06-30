// AgentRuntime (T583, issue-921db054 / I5) — the agent detail "Runtime" tab: a
// READ-ONLY browser over an agent's on-worker runtime files. Left: a collapsible
// directory tree (memory/ git repo, workspace/, events, configs). Right: a file
// preview (plain text), the memory git-log, a "redacted" placeholder for sensitive
// files (plaintext credentials — content never returned), or a metadata-only view
// for binary/special files. When the worker is offline the API returns
// { unavailable: true } and the whole tab degrades to a "Runtime unavailable" notice.
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import {
  useRuntimeList,
  useRuntimeRead,
  useRuntimeGitLog,
  useRuntimeGitDiff,
  isUnavailable,
  type RuntimeEntry,
  type RuntimeReadResp,
} from '@/api/runtime';
import { Skeleton } from '@/components/Skeleton';
import { formatLocalTime } from '@/utils/time';

const SIDEBAR_COLLAPSE_KEY = 'agent-runtime-sidebar-collapsed';

// The memory/ git repo is the only versioned subtree — a path is "memory-scoped"
// (gets the Content / History tabs) when it is the repo dir itself or sits under it.
const MEMORY_ROOT = 'memory';
function isMemoryScoped(path: string): boolean {
  return path === MEMORY_ROOT || path.startsWith(MEMORY_ROOT + '/');
}

type MemoryTab = 'content' | 'history';

type Selected =
  | { kind: 'file'; path: string; name: string; sensitive?: boolean }
  | { kind: 'gitlog'; path: string; name: string };

export function AgentRuntime({ agentId }: { agentId: string }): React.ReactElement {
  const { t } = useTranslation('members');
  const [collapsed, setCollapsed] = useState(
    () => readLocalStorage(SIDEBAR_COLLAPSE_KEY) === '1',
  );
  const [selected, setSelected] = useState<Selected | null>(null);
  // Which tab the memory pane shows. Driven by the selection (a file → its Content,
  // the repo folder → History) but user-overridable, so the git log is ALWAYS one
  // click away instead of being replaced when a file is opened.
  const [memoryTab, setMemoryTab] = useState<MemoryTab>('history');

  const handleSelect = (s: Selected) => {
    setSelected(s);
    if (s.kind === 'gitlog') setMemoryTab('history');
    else if (isMemoryScoped(s.path)) setMemoryTab('content');
  };

  // The root listing drives the whole-tab availability: worker offline → the root
  // itself is unavailable.
  const root = useRuntimeList(agentId, '');

  const toggleCollapsed = () =>
    setCollapsed((c) => {
      const next = !c;
      writeLocalStorage(SIDEBAR_COLLAPSE_KEY, next ? '1' : '0');
      return next;
    });

  if (root.data && isUnavailable(root.data)) {
    return (
      <div data-testid="agent-tabpanel-runtime">
        <RuntimeUnavailable reason={root.data.reason} />
      </div>
    );
  }

  const rootEntries =
    root.data && !isUnavailable(root.data)
      ? root.data.entries.filter((e) => e.name !== '.git')
      : [];

  return (
    <div
      className="runtime-fill-height flex gap-3 overflow-hidden rounded-lg border border-border-base bg-bg-elevated"
      data-testid="agent-tabpanel-runtime"
    >
      {collapsed ? (
        <button
          type="button"
          onClick={toggleCollapsed}
          className="m-2 h-8 w-8 shrink-0 rounded border border-border-base text-text-muted hover:bg-bg-subtle hover:text-text-primary"
          aria-label={t('agentRuntime.runtime.expandTree')}
          data-testid="runtime-sidebar-expand"
          title={t('agentRuntime.runtime.expandTitle')}
        >
          <span aria-hidden="true">»</span>
        </button>
      ) : (
        <aside
          className="flex w-64 shrink-0 flex-col overflow-y-auto border-r border-border-base p-2"
          data-testid="runtime-sidebar"
        >
          <div className="mb-1 flex items-center justify-between px-1">
            <h3 className="text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">{t('agentRuntime.runtime.filesHeading')}</h3>
            <button
              type="button"
              onClick={toggleCollapsed}
              className="h-6 w-6 rounded border border-border-base text-text-muted hover:bg-bg-subtle hover:text-text-primary"
              aria-label={t('agentRuntime.runtime.collapseTree')}
              data-testid="runtime-sidebar-collapse"
              title={t('agentRuntime.runtime.collapseTitle')}
            >
              <span aria-hidden="true">«</span>
            </button>
          </div>
          {root.isLoading ? (
            <div className="space-y-1 p-1" data-testid="runtime-tree-loading">
              <Skeleton height="1.25rem" />
              <Skeleton height="1.25rem" />
              <Skeleton height="1.25rem" />
            </div>
          ) : root.isError ? (
            <RuntimeUnavailable reason={t('agentRuntime.runtime.treeLoadFailed')} />
          ) : (
            <ul role="tree" data-testid="runtime-tree">
              {rootEntries.map((e) => (
                <TreeNode
                  key={e.path}
                  agentId={agentId}
                  entry={e}
                  depth={0}
                  selectedPath={selected?.path}
                  onSelect={handleSelect}
                />
              ))}
            </ul>
          )}
        </aside>
      )}

      <section className="flex min-w-0 flex-1 flex-col overflow-hidden" data-testid="runtime-preview">
        {selected === null ? (
          <p className="py-16 text-center text-xs text-text-muted" data-testid="runtime-preview-empty">
            {t('agentRuntime.runtime.previewEmpty')}
          </p>
        ) : isMemoryScoped(selected.path) ? (
          <MemoryPane agentId={agentId} selected={selected} tab={memoryTab} setTab={setMemoryTab} />
        ) : (
          <div className="min-h-0 flex-1 overflow-auto p-4">
            <FilePreview agentId={agentId} path={selected.path} name={selected.name} />
          </div>
        )}
      </section>
    </div>
  );
}

// MemoryPane — the right pane for any selection inside the memory git repo. A
// persistent tab bar keeps BOTH views reachable (the original UI replaced the git
// log with file content the moment a file was clicked, with no way back):
//   • Content  — the selected file's preview (hidden when the repo folder is selected)
//   • History  — the repo git log, each commit expandable to its diff
function MemoryPane({
  agentId,
  selected,
  tab,
  setTab,
}: {
  agentId: string;
  selected: Selected;
  tab: MemoryTab;
  setTab: (t: MemoryTab) => void;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const hasFile = selected.kind === 'file';
  // No file selected (the memory folder) → History is the only meaningful view.
  const activeTab: MemoryTab = hasFile ? tab : 'history';

  return (
    <div className="flex h-full flex-col" data-testid="runtime-memory-pane">
      <div className="flex shrink-0 items-center gap-1 border-b border-border-base px-3 pt-2">
        {hasFile && (
          <MemoryTabButton active={activeTab === 'content'} onClick={() => setTab('content')} testid="runtime-memory-tab-content">
            {t('agentRuntime.runtime.tabs.content')}
          </MemoryTabButton>
        )}
        <MemoryTabButton active={activeTab === 'history'} onClick={() => setTab('history')} testid="runtime-memory-tab-history">
          {t('agentRuntime.runtime.tabs.history')}
        </MemoryTabButton>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        {activeTab === 'content' && hasFile ? (
          <FilePreview agentId={agentId} path={selected.path} name={selected.name} />
        ) : (
          <GitLogView agentId={agentId} path={MEMORY_ROOT} />
        )}
      </div>
    </div>
  );
}

function MemoryTabButton({
  active,
  onClick,
  testid,
  children,
}: {
  active: boolean;
  onClick: () => void;
  testid: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      data-testid={testid}
      className={`-mb-px border-b-2 px-3 py-1.5 text-xs font-medium ${
        active ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}

// TreeNode — one directory or file row. Directories lazy-load their entries on
// expand (a list() per opened dir). The memory directory (a git repo) also opens
// its git-log in the preview pane when clicked.
function TreeNode({
  agentId,
  entry,
  depth,
  selectedPath,
  onSelect,
}: {
  agentId: string;
  entry: RuntimeEntry;
  depth: number;
  selectedPath: string | undefined;
  onSelect: (s: Selected) => void;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const [open, setOpen] = useState(false);
  const isDir = entry.type === 'directory';
  const isMemory = entry.path === 'memory'; // top-level git repo
  const children = useRuntimeList(agentId, entry.path, isDir && open);

  const onClick = () => {
    if (isDir) {
      setOpen((o) => !o);
      // memory is a git repo → also surface its git-log in the preview pane.
      if (isMemory) onSelect({ kind: 'gitlog', path: entry.path, name: entry.name });
    } else {
      onSelect({ kind: 'file', path: entry.path, name: entry.name, sensitive: entry.sensitive });
    }
  };

  const childEntries =
    children.data && !isUnavailable(children.data)
      ? children.data.entries.filter((e) => e.name !== '.git')
      : [];

  return (
    <li role="treeitem" aria-expanded={isDir ? open : undefined}>
      <button
        type="button"
        onClick={onClick}
        style={{ paddingLeft: `${0.25 + depth * 0.85}rem` }}
        className={`flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-left text-xs hover:bg-bg-subtle ${
          selectedPath === entry.path ? 'bg-bg-subtle font-medium text-accent' : 'text-text-secondary'
        }`}
        data-testid="runtime-tree-row"
        data-path={entry.path}
        data-type={entry.type}
      >
        {isDir ? <Chevron open={open} /> : <span className="w-3 shrink-0" />}
        <EntryIcon entry={entry} />
        <span className="min-w-0 flex-1 truncate">{entry.name}{isDir ? '/' : ''}</span>
        <EntryTag entry={entry} />
      </button>
      {isDir && open && (
        children.isLoading ? (
          <div className="py-1" style={{ paddingLeft: `${0.25 + (depth + 1) * 0.85}rem` }}>
            <Skeleton height="1rem" />
          </div>
        ) : children.data && isUnavailable(children.data) ? (
          <p className="px-2 py-1 text-[0.6875rem] italic text-text-muted" style={{ paddingLeft: `${0.25 + (depth + 1) * 0.85}rem` }}>
            {t('agentRuntime.runtime.unavailable')}
          </p>
        ) : (
          <ul role="group">
            {childEntries.map((c) => (
              <TreeNode
                key={c.path}
                agentId={agentId}
                entry={c}
                depth={depth + 1}
                selectedPath={selectedPath}
                onSelect={onSelect}
              />
            ))}
          </ul>
        )
      )}
    </li>
  );
}

// FilePreview — read() a file and render: a redacted placeholder for sensitive
// files (content never returned), a metadata-only view for binary/special files,
// or the plain-text content (with a truncated note).
function FilePreview({
  agentId,
  path,
  name,
}: {
  agentId: string;
  path: string;
  name: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const read = useRuntimeRead(agentId, path);

  if (read.isLoading) {
    return <Skeleton height="8rem" />;
  }
  if (read.isError || !read.data || isUnavailable(read.data)) {
    return <RuntimeUnavailable reason={read.data && isUnavailable(read.data) ? read.data.reason : undefined} />;
  }
  const d: RuntimeReadResp = read.data;

  return (
    <div data-testid="runtime-file-preview" data-path={path}>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm font-semibold text-text-primary">{name}</span>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.5625rem] font-semibold uppercase tracking-wide text-text-muted">
            {previewTypeLabel(d, t)}
          </span>
        </div>
        <span className="text-[0.6875rem] text-text-muted">
          {formatSize(d.size)}
          {d.mtime ? ` · ${formatLocalTime(d.mtime)}` : ''}
          {d.truncated ? ` · ${t('agentRuntime.runtime.preview.truncated')}` : ''}
        </span>
      </div>

      {d.redacted ? (
        <div
          className="rounded-lg border border-dashed border-warning/50 bg-status-amber-bg px-4 py-6 text-center"
          data-testid="runtime-file-redacted"
        >
          <p className="text-sm font-medium text-status-amber-fg">
            {t('agentRuntime.runtime.preview.redactedTitle')}
          </p>
          <p className="mt-1 text-xs text-status-amber-fg">
            {t('agentRuntime.runtime.preview.redactedBody')}
          </p>
        </div>
      ) : d.image && d.content ? (
        <div
          className="overflow-auto rounded-lg border border-border-base bg-bg-subtle p-3"
          data-testid="runtime-file-image"
        >
          <img
            src={`data:${d.content_type};base64,${d.content}`}
            alt={name}
            className="mx-auto max-w-full rounded"
            data-testid="runtime-file-image-img"
          />
        </div>
      ) : d.binary || d.content === null ? (
        <div
          className="rounded-lg border border-border-base bg-bg-subtle px-4 py-6 text-center"
          data-testid="runtime-file-binary"
        >
          <p className="text-sm text-text-secondary">{t('agentRuntime.runtime.preview.notPreviewable')}</p>
          <p className="mt-1 text-xs text-text-muted">
            {t('agentRuntime.runtime.preview.binaryMeta', { size: formatSize(d.size) })}
          </p>
        </div>
      ) : (
        <pre
          className="overflow-x-auto whitespace-pre-wrap break-words rounded-lg border border-border-base bg-bg-subtle p-3 font-mono text-xs text-text-primary"
          data-testid="runtime-file-content"
        >
          {d.content}
        </pre>
      )}
    </div>
  );
}

// GitLogView — read-only commit history of the memory git repo.
function GitLogView({ agentId, path }: { agentId: string; path: string }): React.ReactElement {
  const { t } = useTranslation('members');
  const log = useRuntimeGitLog(agentId, path);

  return (
    <div data-testid="runtime-gitlog">
      <div className="mb-3 flex items-center gap-2">
        <span className="font-mono text-sm font-semibold text-text-primary">{path}/</span>
        <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.5625rem] font-semibold uppercase tracking-wide text-text-muted">
          {t('agentRuntime.runtime.gitLog.label')}
        </span>
        <span className="ml-auto text-[0.6875rem] text-text-muted">{t('agentRuntime.runtime.gitLog.readOnly')}</span>
      </div>
      {log.isLoading ? (
        <Skeleton height="6rem" />
      ) : log.isError || !log.data || isUnavailable(log.data) ? (
        <RuntimeUnavailable reason={log.data && isUnavailable(log.data) ? log.data.reason : undefined} />
      ) : log.data.commits.length === 0 ? (
        <p className="text-xs italic text-text-muted">{t('agentRuntime.runtime.gitLog.noCommits')}</p>
      ) : (
        <ul className="space-y-1" data-testid="runtime-gitlog-list">
          {log.data.commits.map((c) => (
            <CommitRow key={c.sha} agentId={agentId} path={path} commit={c} />
          ))}
        </ul>
      )}
      <p className="mt-3 text-[0.6875rem] text-text-muted">
        {t('agentRuntime.runtime.gitLog.footer')}
      </p>
    </div>
  );
}

// CommitRow — one git-log entry, expandable to its unified diff (lazy-loaded on first
// open via the gitdiff op, then cached by react-query).
function CommitRow({
  agentId,
  path,
  commit,
}: {
  agentId: string;
  path: string;
  commit: { sha: string; message: string; author: string; date: string };
}): React.ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <li className="border-b border-border-base last:border-0" data-testid="runtime-gitlog-row">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-start gap-2 py-2 text-left hover:bg-bg-subtle"
        data-testid="runtime-gitlog-row-toggle"
        data-sha={commit.sha}
      >
        <Chevron open={open} />
        <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-accent">
          {commit.sha.slice(0, 7)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm text-text-primary">{commit.message}</span>
          <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
            {commit.author}
            {commit.date ? ` · ${formatLocalTime(commit.date)}` : ''}
          </span>
        </span>
      </button>
      {open && <CommitDiff agentId={agentId} path={path} sha={commit.sha} />}
    </li>
  );
}

// CommitDiff — the unified diff of a single commit (`git show`). Fetched only when the
// row is first expanded.
function CommitDiff({
  agentId,
  path,
  sha,
}: {
  agentId: string;
  path: string;
  sha: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const diff = useRuntimeGitDiff(agentId, path, sha);

  if (diff.isLoading) {
    return (
      <div className="pb-2 pl-5">
        <Skeleton height="4rem" />
      </div>
    );
  }
  if (diff.isError || !diff.data || isUnavailable(diff.data)) {
    return (
      <p className="pb-2 pl-5 text-[0.6875rem] italic text-text-muted" data-testid="runtime-gitdiff-error">
        {diff.data && isUnavailable(diff.data) && diff.data.reason ? diff.data.reason : t('agentRuntime.runtime.diff.loadFailed')}
      </p>
    );
  }
  if (diff.data.diff.trim() === '') {
    return (
      <p className="pb-2 pl-5 text-[0.6875rem] italic text-text-muted" data-testid="runtime-gitdiff-empty">
        {t('agentRuntime.runtime.diff.noChanges')}
      </p>
    );
  }
  return (
    <div className="pb-2 pl-5" data-testid="runtime-gitdiff">
      <pre className="overflow-x-auto whitespace-pre rounded-lg border border-border-base bg-bg-base p-3 font-mono text-[0.6875rem] leading-relaxed text-text-secondary">
        {renderDiffLines(diff.data.diff)}
      </pre>
      {diff.data.truncated && (
        <p className="mt-1 text-[0.625rem] italic text-text-muted">{t('agentRuntime.runtime.diff.truncated')}</p>
      )}
    </div>
  );
}

// renderDiffLines colourises a unified diff (added/removed/hunk header) for readability.
function renderDiffLines(diff: string): React.ReactNode[] {
  return diff.split('\n').map((line, i) => {
    let cls = 'text-text-secondary';
    if (line.startsWith('+') && !line.startsWith('+++')) cls = 'text-status-green-fg';
    else if (line.startsWith('-') && !line.startsWith('---')) cls = 'text-danger';
    else if (line.startsWith('@@')) cls = 'text-accent';
    else if (line.startsWith('diff ') || line.startsWith('index ') || line.startsWith('+++') || line.startsWith('---'))
      cls = 'text-text-muted';
    return (
      <span key={i} className={`block ${cls}`}>
        {line || ' '}
      </span>
    );
  });
}

function RuntimeUnavailable({ reason }: { reason?: string }): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <div className="flex flex-col items-center justify-center px-4 py-16 text-center" data-testid="runtime-unavailable">
      <svg viewBox="0 0 24 24" className="mb-2 h-8 w-8 text-text-muted" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M5.6 5.6l12.8 12.8" strokeLinecap="round" />
      </svg>
      <p className="text-sm font-semibold text-text-secondary">{t('agentRuntime.runtime.unavailableTitle')}</p>
      <p className="mt-1 max-w-md text-xs text-text-muted">
        {reason && reason.trim() !== ''
          ? reason
          : t('agentRuntime.runtime.unavailableDefault')}
      </p>
    </div>
  );
}

// ── small helpers ────────────────────────────────────────────────────────────

function Chevron({ open }: { open: boolean }): React.ReactElement {
  return (
    <svg
      viewBox="0 0 12 12"
      className={`h-3 w-3 shrink-0 text-text-muted transition-transform ${open ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path d="M4.5 2.5 8 6l-3.5 3.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function EntryIcon({ entry }: { entry: RuntimeEntry }): React.ReactElement {
  if (entry.sensitive) {
    // lock glyph for sensitive / special files
    return (
      <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0 text-text-muted" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
        <rect x="3.5" y="7" width="9" height="6" rx="1" />
        <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" />
      </svg>
    );
  }
  if (entry.type === 'directory') {
    return (
      <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0 text-text-muted" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
        <path d="M2 4.5A1 1 0 0 1 3 3.5h3l1.2 1.5H13a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z" strokeLinejoin="round" />
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0 text-text-muted" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <path d="M4 2.5h5l3 3V13a.5.5 0 0 1-.5.5h-7A.5.5 0 0 1 4 13z" strokeLinejoin="round" />
    </svg>
  );
}

// A trailing tag on a tree row: git for the memory repo, lock for sock/lock files,
// redacted for other sensitive files, else the size.
function EntryTag({ entry }: { entry: RuntimeEntry }): React.ReactElement {
  const { t } = useTranslation('members');
  const muted = 'shrink-0 text-[0.625rem] text-text-muted';
  if (entry.path === 'memory') return <span className={muted}>{t('agentRuntime.runtime.tag.git')}</span>;
  if (entry.type === 'directory') return <span className="w-0" />;
  if (/\.(sock|lock)$/.test(entry.name)) return <span className={muted}>{t('agentRuntime.runtime.tag.lock')}</span>;
  if (entry.sensitive) return <span className={muted}>{t('agentRuntime.runtime.tag.redacted')}</span>;
  return <span className={`${muted} font-mono`}>{formatSize(entry.size)}</span>;
}

function previewTypeLabel(d: RuntimeReadResp, t: TFunction): string {
  if (d.redacted) return t('agentRuntime.runtime.typeLabel.redacted');
  if (d.image) return t('agentRuntime.runtime.typeLabel.image');
  if (d.binary) return t('agentRuntime.runtime.typeLabel.binary');
  const ct = (d.content_type || '').toLowerCase();
  if (ct.includes('markdown')) return t('agentRuntime.runtime.typeLabel.markdown');
  if (ct.includes('json')) return t('agentRuntime.runtime.typeLabel.json');
  if (ct.includes('text') || ct === '') return t('agentRuntime.runtime.typeLabel.text');
  return ct;
}

// formatSize → compact byte label ("285", "2.1k", "18k", "1.2M") matching the mockup.
function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '';
  if (bytes < 1000) return String(bytes);
  if (bytes < 1_000_000) {
    const k = bytes / 1000;
    return `${k < 10 ? k.toFixed(1) : Math.round(k)}k`;
  }
  const m = bytes / 1_000_000;
  return `${m < 10 ? m.toFixed(1) : Math.round(m)}M`;
}

function readLocalStorage(key: string): string | null {
  try {
    return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null;
  } catch {
    return null;
  }
}
function writeLocalStorage(key: string, value: string): void {
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem(key, value);
  } catch {
    // ignore (private mode / disabled storage)
  }
}
