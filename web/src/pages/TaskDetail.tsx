import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useDisplayNameResolver } from '@/api/members';
import { MarkdownMessage } from '@/components/MarkdownMessage';
import { TypeChip } from '@/components/TypeChip';
import { useTask } from '@/api/tasks';
import { useProject } from '@/api/projects';
import { TaskEditModal } from '@/components/TaskEditModal';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { TaskDetailSidebar } from '@/components/TaskDetailSidebar';
import { Breadcrumb } from '@/components/Breadcrumb';

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
  const [editOpen, setEditOpen] = useState(false);

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

  return (
    <section className="flex h-full flex-col" data-testid="page-TaskDetail" data-task-id={tk.id}>
      <div className="mb-2">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: project.data?.name || 'Project', to: `/projects/${encodeURIComponent(tk.project_id)}` },
            { label: 'Tasks' },
            { label: tk.org_ref ? `${tk.org_ref} - ${tk.title || tk.id}` : tk.title || tk.id },
          ]}
        />
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden lg:flex-row">
        {/* main column — title + description + conversation */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="border-b border-border-base pb-3">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-xl font-semibold">
                {tk.org_ref && <span className="text-text-muted" data-testid="task-org-ref">{tk.org_ref} · </span>}
                {tk.title || tk.id}
              </h2>
              <TypeChip kind="task" />
            </div>
          </header>

          {tk.description ? (
            // @oopslink: render the description as markdown (reuse MarkdownMessage)
            // and cap its height so a long description scrolls internally instead
            // of pushing the conversation off-screen. tabIndex makes the scroll
            // region keyboard-reachable (WCAG 2.1.1), per Tester2's a11y flag.
            <div
              className="mt-4 max-h-64 overflow-y-auto text-sm text-text-secondary"
              data-testid="task-description"
              tabIndex={0}
              role="region"
              aria-label="Task description"
            >
              <MarkdownMessage content={tk.description} />
            </div>
          ) : (
            <p className="mt-4 text-sm italic text-text-muted">No description.</p>
          )}

          <WorkItemConversation ownerRef={`pm://tasks/${tk.id}`} bannerLabel={tk.title || tk.id} />
        </div>

        {/* right sidebar — 2-section TaskDetail layout (read-only display top /
            read-only bottom). The ONLY edit path is the Edit-Task modal. */}
        <div className="shrink-0 overflow-y-auto lg:w-72">
          <TaskDetailSidebar
            task={tk}
            projectName={project.data?.name}
            assigneeName={resolvedAssigneeName}
            onEdit={() => setEditOpen(true)}
            editable={!isTerminal}
          />
          {/* ADR-0046 D5: "stuck" is no longer a status — it's a blocked_reason
              annotation on a RUNNING task. Show a solid amber chip (theme-
              independent amber-100/amber-800, AA ≥4.5 in both light + dark, no
              alpha-tint — mirrors the chip pattern in tagColors.ts). */}
          {status === 'running' && tk.blocked_reason && (
            <div
              className="mt-2 rounded bg-amber-100 px-2 py-1 text-xs font-medium text-amber-800"
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
    </section>
  );
}
