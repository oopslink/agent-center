import type React from 'react';
import { useMemo, useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useAgents } from '@/api/agents';
import { useMembers, useDisplayNameResolver } from '@/api/members';
import { EntityRef } from '@/components/EntityRef';
import { TypeChip } from '@/components/TypeChip';
import {
  useAssignTask,
  useBlockTask,
  useCancelTask,
  useCompleteTask,
  useReopenTask,
  useStartTask,
  useTask,
  useUnassignTask,
  useUnblockTask,
  useVerifyTask,
} from '@/api/tasks';
import { useProject } from '@/api/projects';
import { TaskEditModal } from '@/components/TaskEditModal';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { Breadcrumb } from '@/components/Breadcrumb';

// TaskDetail (/projects/:projectId/tasks/:id). v2.7 ProjectManager BC:
// the task is project-scoped and driven entirely by its projection.
// The new state machine actions each POST to a sub-route and return the
// refreshed task. Metadata edits via PATCH.
export default function TaskDetail(): React.ReactElement {
  const { projectId = '', id = '' } = useParams<{ projectId: string; id: string }>();
  const task = useTask(projectId, id);
  // v2.7 #186-2: show the project's display name (not its ULID) in the
  // breadcrumb + project link.
  const project = useProject(projectId);
  // v2.7 #192: resolve the assignee ref to a display name (raw ref on hover);
  // an unresolved ref (e.g. a deleted assignee) renders "(deleted)".
  const resolveName = useDisplayNameResolver();
  const [editOpen, setEditOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const [blockOpen, setBlockOpen] = useState(false);
  // v2.7 #186-3a: lifecycle transitions are presented as a dropdown anchored
  // to the status badge instead of a scattered row of buttons.
  const [menuOpen, setMenuOpen] = useState(false);

  const assign = useAssignTask(projectId, id);
  const start = useStartTask(projectId, id);
  const block = useBlockTask(projectId, id);
  const unblock = useUnblockTask(projectId, id);
  const complete = useCompleteTask(projectId, id);
  const verify = useVerifyTask(projectId, id);
  const cancel = useCancelTask(projectId, id);
  const unassign = useUnassignTask(projectId, id);
  const reopen = useReopenTask(projectId, id);

  if (task.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-TaskDetail">
        Loading task…
      </section>
    );
  }
  if (task.isError) {
    return (
      <section className="space-y-3" data-testid="page-TaskDetail">
        <p className="text-sm text-danger" data-testid="task-not-found">
          {(task.error as Error).message}
        </p>
        <OrgLink to={`/projects/${encodeURIComponent(projectId)}`} className="text-accent hover:underline">
          Back to project
        </OrgLink>
      </section>
    );
  }
  if (!task.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-TaskDetail">
        Task lookup failed.
      </section>
    );
  }

  const tk = task.data;
  const status = tk.status;
  const isTerminal = status === 'canceled';
  const canCancel = status !== 'canceled' && status !== 'verified' && status !== 'completed';

  // v2.7 #186-3a: the lifecycle transitions valid for the current status.
  // Each becomes a dropdown item; the status badge is the trigger.
  type TaskAction = { testId: string; label: string; onClick: () => void; danger?: boolean; pending?: boolean };
  const actions: TaskAction[] = [];
  switch (status) {
    case 'open':
      actions.push({ testId: 'task-assign-button', label: 'Assign', onClick: () => setAssignOpen(true) });
      break;
    case 'assigned':
      actions.push({ testId: 'task-start-button', label: 'Start', onClick: () => start.mutate(), pending: start.isPending });
      actions.push({ testId: 'task-unassign-button', label: 'Unassign', onClick: () => unassign.mutate(), pending: unassign.isPending });
      break;
    case 'running':
      actions.push({ testId: 'task-complete-button', label: 'Complete', onClick: () => complete.mutate(), pending: complete.isPending });
      actions.push({ testId: 'task-block-button', label: 'Block', onClick: () => setBlockOpen(true) });
      break;
    case 'blocked':
      actions.push({ testId: 'task-unblock-button', label: 'Unblock', onClick: () => unblock.mutate(), pending: unblock.isPending });
      break;
    case 'completed':
      actions.push({ testId: 'task-verify-button', label: 'Verify', onClick: () => verify.mutate(), pending: verify.isPending });
      actions.push({ testId: 'task-reopen-button', label: 'Reopen', onClick: () => reopen.mutate(), pending: reopen.isPending });
      break;
    case 'verified':
      actions.push({ testId: 'task-reopen-button', label: 'Reopen', onClick: () => reopen.mutate(), pending: reopen.isPending });
      break;
  }
  if (canCancel) {
    actions.push({ testId: 'task-cancel-button', label: 'Cancel', onClick: () => cancel.mutate(), danger: true, pending: cancel.isPending });
  }

  const actionError =
    (assign.error ?? start.error ?? block.error ?? unblock.error ??
      complete.error ?? verify.error ?? cancel.error ?? unassign.error ??
      reopen.error) as Error | null;

  return (
    <section className="flex h-full flex-col" data-testid="page-TaskDetail" data-task-id={tk.id}>
      <div className="mb-2">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: project.data?.name || 'Project', to: `/projects/${encodeURIComponent(tk.project_id)}` },
            { label: 'Tasks' },
            { label: tk.title || tk.id },
          ]}
        />
      </div>
      <header className="flex items-start justify-between border-b border-border-base pb-3">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold">{tk.title || tk.id}</h2>
            <TypeChip kind="task" />
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
            {/* v2.7 #186-3a: the status badge is the transition dropdown trigger. */}
            <div className="relative">
              <button
                type="button"
                onClick={() => setMenuOpen((v) => !v)}
                className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary hover:bg-border-base"
                data-testid="task-status"
                aria-haspopup="menu"
                aria-expanded={menuOpen && actions.length > 0}
              >
                {status}
              </button>
              {menuOpen && actions.length > 0 && (
                <ul
                  className="absolute left-0 z-10 mt-1 min-w-[10rem] rounded border border-border-base bg-bg-elevated py-1 shadow-lg"
                  data-testid="task-status-menu"
                  role="menu"
                >
                  {actions.map((a) => (
                    <li key={a.testId} role="none">
                      <button
                        type="button"
                        role="menuitem"
                        disabled={a.pending}
                        onClick={() => {
                          setMenuOpen(false);
                          a.onClick();
                        }}
                        data-testid={a.testId}
                        className={`block w-full px-3 py-1.5 text-left text-xs font-medium normal-case hover:bg-bg-subtle disabled:opacity-50 ${a.danger ? 'text-danger' : 'text-text-primary'}`}
                      >
                        {a.pending ? `${a.label}…` : a.label}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            {tk.assignee && (
              <EntityRef
                id={tk.assignee}
                name={resolveName(tk.assignee) === tk.assignee ? undefined : resolveName(tk.assignee)}
                testId="task-assignee"
              />
            )}
            {tk.blocked_reason && status === 'blocked' && (
              <span className="text-danger" data-testid="task-blocked-reason">
                blocked: {tk.blocked_reason}
              </span>
            )}
            {tk.project_id && (
              <OrgLink
                to={`/projects/${encodeURIComponent(tk.project_id)}`}
                className="text-accent hover:underline"
                data-testid="task-project-link"
              >
                project · {project.data?.name || tk.project_id}
              </OrgLink>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!isTerminal && (
            <button
              type="button"
              onClick={() => setEditOpen(true)}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="task-edit-button"
            >
              Edit
            </button>
          )}
        </div>
      </header>

      {actionError && (
        <p className="mt-2 text-xs text-danger" data-testid="task-action-error">
          {actionError.message}
        </p>
      )}

      {tk.description ? (
        <p className="mt-4 whitespace-pre-wrap text-sm text-text-secondary" data-testid="task-description">
          {tk.description}
        </p>
      ) : (
        <p className="mt-4 text-sm italic text-text-muted">No description.</p>
      )}

      <WorkItemConversation ownerRef={`pm://tasks/${tk.id}`} bannerLabel={tk.title || tk.id} />

      {editOpen && (
        <TaskEditModal projectId={projectId} task={tk} onClose={() => setEditOpen(false)} />
      )}
      {assignOpen && (
        <AssignModal
          pending={assign.isPending}
          error={assign.error as Error | null}
          onClose={() => setAssignOpen(false)}
          onSubmit={async (assignee) => {
            try {
              await assign.mutateAsync({ assignee });
              setAssignOpen(false);
            } catch {
              // surfaced
            }
          }}
        />
      )}
      {blockOpen && (
        <BlockModal
          pending={block.isPending}
          error={block.error as Error | null}
          onClose={() => setBlockOpen(false)}
          onSubmit={async (reason) => {
            try {
              await block.mutateAsync({ reason });
              setBlockOpen(false);
            } catch {
              // surfaced
            }
          }}
        />
      )}
    </section>
  );
}

// v2.7 #186-5a/5b: searchable assignee picker (#167 pattern) over agents +
// human members. Selecting a candidate submits the org-scoped identity ref:
// agent = `agent:<member-id>` (agent.identity_member_id, the business id — the
// backend resolves member→entity via the bridge); human = `user:<identity_id>`
// (PM-tracking, no execution). Replaces the old free-text agent-name input.
function AssignModal({
  pending,
  error,
  onClose,
  onSubmit,
}: {
  pending: boolean;
  error: Error | null;
  onClose: () => void;
  onSubmit: (assignee: string) => void;
}): React.ReactElement {
  const agents = useAgents();
  const members = useMembers();
  const [q, setQ] = useState('');
  const candidates = useMemo(() => {
    const out: { ref: string; label: string; kind: 'agent' | 'human' }[] = [];
    for (const a of agents.data ?? []) {
      out.push({ ref: `agent:${a.identity_member_id || a.id}`, label: a.name || a.id, kind: 'agent' });
    }
    for (const m of members.data ?? []) {
      if (m.kind !== 'user') continue;
      out.push({ ref: `user:${m.identity_id}`, label: m.display_name || m.identity_id, kind: 'human' });
    }
    const f = q.trim().toLowerCase();
    return f ? out.filter((c) => c.label.toLowerCase().includes(f) || c.ref.toLowerCase().includes(f)) : out;
  }, [agents.data, members.data, q]);
  return (
    <Modal testId="task-assign-modal" title="Assign task" onClose={onClose}>
      <input
        data-testid="task-assign-search"
        className={modalInputClass}
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search agents or people…"
        autoFocus
      />
      <ul className="mt-2 max-h-60 overflow-y-auto" data-testid="task-assign-candidates">
        {candidates.length === 0 && (
          <li className="px-2 py-1.5 text-xs text-text-muted" data-testid="task-assign-empty">
            No matching agents or people.
          </li>
        )}
        {candidates.map((c) => (
          <li key={c.ref}>
            <button
              type="button"
              disabled={pending}
              onClick={() => onSubmit(c.ref)}
              data-testid="task-assign-candidate"
              data-assignee-ref={c.ref}
              data-kind={c.kind}
              className="flex w-full items-center justify-between rounded px-2 py-1.5 text-left text-sm hover:bg-bg-subtle disabled:opacity-50"
            >
              <span>{c.label}</span>
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-xs uppercase text-text-muted">{c.kind}</span>
            </button>
          </li>
        ))}
      </ul>
      {error && (
        <p className="mt-2 text-xs text-danger" data-testid="task-assign-error">
          {error.message}
        </p>
      )}
      <div className="mt-3 flex justify-end">
        <button
          type="button"
          onClick={onClose}
          className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
          data-testid="task-assign-cancel"
        >
          Cancel
        </button>
      </div>
    </Modal>
  );
}

function BlockModal({
  pending,
  error,
  onClose,
  onSubmit,
}: {
  pending: boolean;
  error: Error | null;
  onClose: () => void;
  onSubmit: (reason: string) => void;
}): React.ReactElement {
  const [reason, setReason] = useState('');
  const trimmed = reason.trim();
  const canSubmit = trimmed.length > 0 && !pending;
  return (
    <Modal testId="task-block-modal" title="Block task" onClose={onClose}>
      <label className="mb-1 block text-xs font-medium text-text-primary">
        Reason<span className="ml-1 text-danger">*</span>
      </label>
      <textarea
        data-testid="task-block-input"
        className={modalInputClass}
        rows={3}
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        placeholder="Why is this blocked?"
      />
      {error && (
        <p className="mt-2 text-xs text-danger" data-testid="task-block-error">
          {error.message}
        </p>
      )}
      <ModalFooter
        onClose={onClose}
        submitLabel={pending ? 'Blocking…' : 'Block'}
        submitTestId="task-block-submit"
        disabled={!canSubmit}
        onSubmit={() => canSubmit && onSubmit(trimmed)}
      />
    </Modal>
  );
}

function Modal({
  testId,
  title,
  onClose,
  children,
}: {
  testId: string;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid={testId}
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
          >
            X
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

function ModalFooter({
  onClose,
  onSubmit,
  submitLabel,
  submitTestId,
  disabled,
}: {
  onClose: () => void;
  onSubmit: () => void;
  submitLabel: string;
  submitTestId: string;
  disabled: boolean;
}): React.ReactElement {
  return (
    <div className="mt-4 flex justify-end gap-2">
      <button
        type="button"
        className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
        onClick={onClose}
      >
        Cancel
      </button>
      <button
        type="button"
        disabled={disabled}
        className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
        data-testid={submitTestId}
        onClick={onSubmit}
      >
        {submitLabel}
      </button>
    </div>
  );
}

const modalInputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
