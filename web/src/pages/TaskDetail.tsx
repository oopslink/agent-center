import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useDisplayNameResolver } from '@/api/members';
import { CollapsibleDescription } from '@/components/CollapsibleDescription';
import { Skeleton } from '@/components/Skeleton';
import { TypeChip } from '@/components/TypeChip';
import { useTask } from '@/api/tasks';
import { useProject } from '@/api/projects';
import { usePlan } from '@/api/plans';
import { useIssue } from '@/api/issues';
import { TaskEditModal } from '@/components/TaskEditModal';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { useConversationByOwnerRef } from '@/api/conversations';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { TaskDetailSidebar } from '@/components/TaskDetailSidebar';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';
import { TaskAttachments } from '@/components/AttachmentsSection';
import { Breadcrumb } from '@/components/Breadcrumb';
import { MobileWorkItemBar, MobileDetailsContent, useIsMobile } from '@/components/WorkItemMobileMeta';

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
  // T193: resolve the issue this task was derived from (for the sidebar's
  // "Related Issue" row). Gated on derived_from_issue — useIssue no-ops without
  // an id, so a task with no provenance fires no request (no N+1).
  const derivedIssue = useIssue(projectId, task.data?.derived_from_issue || undefined);
  // T184: resolve the task's bound conversation so the shared col④ sidebar
  // (Participants / Threads / Files) can render for it, same as channels/DMs.
  const conv = useConversationByOwnerRef(`pm://tasks/${id}`);
  const [editOpen, setEditOpen] = useState(false);
  // T309: mobile "Show info" toggle (description/attachments/details collapsed
  // by default so the chat fills the screen).
  const [showInfo, setShowInfo] = useState(false);
  // T145: on mobile the title is the big <h2>; drop it from the breadcrumb leaf
  // (show just the org_ref / "Task") so the title isn't rendered twice.
  const isMobile = useIsMobile();

  if (task.isLoading) {
    return (
      <section className="space-y-3" role="status" data-testid="page-TaskDetail">
        <Skeleton width="12rem" height="1.5rem" />
        <Skeleton height="4rem" />
        <span className="sr-only">Loading task…</span>
      </section>
    );
  }
  if (task.isError) {
    return (
      <section className="space-y-3" role="alert" data-testid="page-TaskDetail">
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
  // T193: pass the resolved derived issue to the sidebar (ref + title + id for the
  // link). Only when the task carries derived_from_issue AND the issue loaded.
  const derivedIssueForSidebar =
    tk.derived_from_issue && derivedIssue.data
      ? {
          id: derivedIssue.data.id,
          org_ref: derivedIssue.data.org_ref,
          title: derivedIssue.data.title,
        }
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

          {/* T309 (@oopslink mockup): on MOBILE the secondary info collapses behind
              a compact bar (status + assignee + Show info + Edit) so the CHAT fills
              the rest; on DESKTOP the description + attachments stay inline above the
              conversation (the sidebar carries the details). */}
          {isMobile ? (
            <>
              <MobileWorkItemBar
                kind="task"
                status={status}
                statusChangedAt={tk.status_changed_at}
                assignee={tk.assignee ?? null}
                assigneeName={resolvedAssigneeName}
                showInfo={showInfo}
                onToggleInfo={() => setShowInfo((v) => !v)}
                editable={!isTerminal}
                onEdit={() => setEditOpen(true)}
              />
              {showInfo && (
                <div
                  className="mb-3 rounded-lg border border-border-base bg-bg-elevated p-3"
                  data-testid="wi-mobile-info"
                >
                  {tk.description ? (
                    <CollapsibleDescription content={tk.description} testId="task-description" ariaLabel="Task description" />
                  ) : (
                    <p className="text-sm italic text-text-muted">No description.</p>
                  )}
                  <div className="mt-3 border-t border-border-base pt-3">
                    <TaskAttachments projectId={tk.project_id} taskId={tk.id} />
                  </div>
                  <div className="mt-3 border-t border-border-base pt-3">
                    <MobileDetailsContent
                      kind="task"
                      projectId={tk.project_id}
                      projectName={project.data?.name}
                      itemId={tk.id}
                      orgRef={tk.org_ref}
                      createdAt={tk.created_at}
                      tags={tk.tags ?? []}
                    />
                    {planForSidebar && (
                      <div className="flex items-center justify-between gap-3 pt-1">
                        <span className="shrink-0 text-xs uppercase tracking-wide text-text-muted">Plan</span>
                        <OrgLink
                          to={`/projects/${encodeURIComponent(tk.project_id)}/plans/${encodeURIComponent(planForSidebar.id)}`}
                          className="min-w-0 truncate text-right text-xs font-medium text-accent hover:underline"
                          data-testid="wi-mobile-plan-link"
                          data-plan-id={planForSidebar.id}
                        >
                          {planForSidebar.name}
                        </OrgLink>
                      </div>
                    )}
                  </div>
                </div>
              )}
              <div className="flex flex-1 flex-col">
                <WorkItemConversation ownerRef={`pm://tasks/${tk.id}`} bannerLabel={tk.title || tk.id} ownerCode={tk.org_ref} />
              </div>
            </>
          ) : (
            <>
              {tk.description ? (
                // T179: long descriptions default-collapse (Show more); expanding
                // reveals the full markdown in a height-capped, scrollable region.
                <CollapsibleDescription content={tk.description} testId="task-description" ariaLabel="Task description" />
              ) : (
                <p className="mt-4 text-sm italic text-text-muted">No description.</p>
              )}
              <div className="mt-4 border-t border-border-base pt-3">
                <TaskAttachments projectId={tk.project_id} taskId={tk.id} />
              </div>
              <WorkItemConversation ownerRef={`pm://tasks/${tk.id}`} bannerLabel={tk.title || tk.id} ownerCode={tk.org_ref} />
            </>
          )}
        </div>

        {/* metadata sidebar — 2-section TaskDetail layout (read-only display top /
            read-only bottom). The ONLY edit path is the Edit-Task modal.
            T145: hidden on mobile (<md) — the mobile meta summary + Details panel
            above replace it so status/assignee/plan aren't buried at the bottom.
            T324: this metadata (DETAILS) rail stays on the RIGHT; the
            conversation's Participants/Threads/Files panel is now embedded INSIDE
            the chat box (WorkItemConversation's right pane), not the shell col④.
            Below md the rail stacks after the conversation as before. */}
        <div className="hidden shrink-0 overflow-y-auto md:block lg:w-72">
          <TaskDetailSidebar
            task={tk}
            projectName={project.data?.name}
            assigneeName={resolvedAssigneeName}
            plan={planForSidebar}
            derivedIssue={derivedIssueForSidebar}
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

      {/* T324: on MOBILE the conversation's Participants/Threads/Files panel
          stays in the col④ bottom sheet; on DESKTOP it is embedded inside the
          chat box (WorkItemConversation's right pane), so we mount the col④
          panel for mobile only — avoiding a duplicate + an empty desktop col④. */}
      {conv.data && isMobile && (
        <ContextPanel>
          <ConversationSidebar conversationId={conv.data.id} participants={conv.data.participants ?? []} />
        </ContextPanel>
      )}
    </section>
    </SenderSidebarProvider>
  );
}
