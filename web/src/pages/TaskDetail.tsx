import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useDisplayNameResolver } from '@/api/members';
import { CollapsibleDescription } from '@/components/CollapsibleDescription';
import { TypeChip } from '@/components/TypeChip';
import { useTask } from '@/api/tasks';
import { useProject } from '@/api/projects';
import { usePlan } from '@/api/plans';
import { TaskEditModal } from '@/components/TaskEditModal';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { useConversationByOwnerRef } from '@/api/conversations';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { TaskDetailSidebar } from '@/components/TaskDetailSidebar';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';
import { TaskAttachments } from '@/components/AttachmentsSection';
import { Breadcrumb } from '@/components/Breadcrumb';
import { MobileMetaSummary, MobileDetailsPanel, useIsMobile } from '@/components/WorkItemMobileMeta';

// TaskDetail (/projects/:projectId/tasks/:id). v2.7 ProjectManager BC:
// the task is project-scoped and driven entirely by its projection.
// v2.8.1 #281 readonly: per @oopslink, a task may ONLY be edited via the Edit
// Task modal (TaskEditModal — one atomic PATCH of title/desc/status/assignee/
// tags). The sidebar is a pure read-only DISPLAY; there are no inline status /
// assignee / tag edit controls here anymore. The single edit path is the modal,
// which owns its own useUpdateTask hook.
export default function TaskDetail(): React.ReactElement {
  const { projectId = '', id = '' } = useParams<{ projectId: string; id: string }>();
  const task = useTask(projectId, id);
  // v2.7 #186-2: show the project's display name (not its ULID) in the
  // breadcrumb + project link.
  const project = useProject(projectId);
  // v2.7 #192: resolve the assignee ref to a display name (raw ref on hover);
  // an unresolved ref (e.g. a deleted assignee) renders "(deleted)".
  const resolveName = useDisplayNameResolver();
  // T106: fetch the owning plan (when the task is in one) to show + link it in
  // the sidebar. Gated on plan_id (usePlan no-ops without it). The built-in
  // assignment pool is excluded below — it is not a user-facing plan.
  const plan = usePlan(projectId, task.data?.plan_id);
  // T184: resolve the task's bound conversation so the shared col④ sidebar
  // (Participants / Threads / Files) can render for it, same as channels/DMs.
  const conv = useConversationByOwnerRef(`pm://tasks/${id}`);
  const [editOpen, setEditOpen] = useState(false);
  // T145: on mobile the title is the big <h2>; drop it from the breadcrumb leaf
  // (show just the org_ref / "Task") so the title isn't rendered twice.
  const isMobile = useIsMobile();

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
  // The Edit-Task button hides on a terminal (discarded) task — nothing to edit.
  const isTerminal = status === 'discarded';

  const resolvedAssigneeName = tk.assignee ? resolveName(tk.assignee) : '';
  // T106: pass the owning plan to the sidebar ONLY when it is a structured plan
  // (exclude the built-in assignment pool — not a user-facing plan) and loaded.
  const planForSidebar =
    plan.data && plan.data.is_builtin !== true
      ? { id: plan.data.id, name: plan.data.name }
      : undefined;

  return (
    // T102: a page-level SenderSidebarProvider so the sidebar's clickable assignee
    // opens the shared agent activity sidebar (the conversation has its own nested
    // provider for @mentions; this serves the rest of the page).
    <SenderSidebarProvider>
    <section className="flex h-full flex-col" data-testid="page-TaskDetail" data-task-id={tk.id}>
      <div className="mb-2">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: project.data?.name || 'Project', to: `/projects/${encodeURIComponent(tk.project_id)}` },
            { label: 'Tasks' },
            {
              label: isMobile
                ? tk.org_ref || 'Task'
                : tk.org_ref
                  ? `${tk.org_ref} - ${tk.title || tk.id}`
                  : tk.title || tk.id,
            },
          ]}
        />
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden lg:flex-row">
        {/* main column — title + description + conversation */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="border-b border-border-base pb-3">
            <div className="flex flex-wrap items-center gap-2">
              {/* T145: clamp the title to 2 lines on mobile so it doesn't fill the
                  whole first screen (full title on ≥md). */}
              <h2 className="line-clamp-2 text-lg font-semibold md:line-clamp-none md:text-xl">
                {tk.org_ref && <span className="text-text-muted" data-testid="task-org-ref">{tk.org_ref} · </span>}
                {tk.title || tk.id}
              </h2>
              <TypeChip kind="task" />
            </div>
          </header>

          {/* T145: mobile-only meta summary (status · assignee · plan) ABOVE the
              description so the key metadata is on the first screen (the desktop
              sidebar below is hidden <md). */}
          <MobileMetaSummary
            status={status}
            statusChangedAt={tk.status_changed_at}
            assignee={tk.assignee ?? null}
            assigneeName={resolvedAssigneeName}
            projectId={tk.project_id}
            plan={planForSidebar}
          />

          {tk.description ? (
            // T179: long descriptions default-collapse (Show more) so they don't
            // push the conversation off-screen on mobile; expanding reveals the
            // full markdown in a height-capped, keyboard-scrollable region.
            <CollapsibleDescription
              content={tk.description}
              testId="task-description"
              ariaLabel="Task description"
            />
          ) : (
            <p className="mt-4 text-sm italic text-text-muted">No description.</p>
          )}

          {/* v2.10.0 [T73]: task-scoped attachments (list + upload + download). */}
          <div className="mt-4 border-t border-border-base pt-3">
            <TaskAttachments projectId={tk.project_id} taskId={tk.id} />
          </div>

          {/* T145: mobile-only collapsible "Details" (compact single-line rows +
              Edit), moved down below the summary; desktop keeps the sidebar. */}
          <MobileDetailsPanel
            kind="task"
            projectId={tk.project_id}
            projectName={project.data?.name}
            itemId={tk.id}
            orgRef={tk.org_ref}
            createdAt={tk.created_at}
            tags={tk.tags ?? []}
            editable={!isTerminal}
            onEdit={() => setEditOpen(true)}
          />

          <WorkItemConversation ownerRef={`pm://tasks/${tk.id}`} bannerLabel={tk.title || tk.id} />
        </div>

        {/* right sidebar — 2-section TaskDetail layout (read-only display top /
            read-only bottom). The ONLY edit path is the Edit-Task modal.
            T145: hidden on mobile (<md) — the mobile meta summary + Details panel
            above replace it so status/assignee/plan aren't buried at the bottom. */}
        <div className="hidden shrink-0 overflow-y-auto md:block lg:w-72">
          <TaskDetailSidebar
            task={tk}
            projectName={project.data?.name}
            assigneeName={resolvedAssigneeName}
            plan={planForSidebar}
            onEdit={() => setEditOpen(true)}
            editable={!isTerminal}
          />
          {/* ADR-0046 D5: "stuck" is no longer a status — it's a blocked_reason
              annotation on a RUNNING task. Show a solid amber chip (theme-
              independent amber-100/amber-800, AA ≥4.5 in both light + dark, no
              alpha-tint — mirrors the chip pattern in tagColors.ts). */}
          {status === 'running' && tk.blocked_reason && (
            <div
              className="mt-2 rounded bg-status-amber-bg px-2 py-1 text-xs font-medium text-status-amber-fg"
              data-testid="task-blocked-reason"
            >
              Stuck: {tk.blocked_reason}
            </div>
          )}
        </div>
      </div>

      {editOpen && (
        <TaskEditModal projectId={projectId} task={tk} onClose={() => setEditOpen(false)} />
      )}

      {/* T184: the task's conversation gets the shared col④ sidebar
          (Participants / Threads / Files) — same as channels/DMs/issues/plans. */}
      {conv.data && (
        <ContextPanel>
          <ConversationSidebar conversationId={conv.data.id} participants={conv.data.participants ?? []} />
        </ContextPanel>
      )}
    </section>
    </SenderSidebarProvider>
  );
}
