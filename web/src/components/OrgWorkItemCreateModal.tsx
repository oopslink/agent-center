import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useProjects } from '@/api/projects';
import { useModalA11y } from './useModalA11y';
import { IssueCreateModal } from './IssueCreateModal';
import { TaskCreateModal } from './TaskCreateModal';

interface Props {
  kind: 'issue' | 'task';
  onClose: () => void;
}

// OrgWorkItemCreateModal — the OrgWorkItems list is cross-project (#258), so
// creating an issue/task from here first needs a project. Pick a project →
// delegate to the per-project IssueCreateModal / TaskCreateModal, which own the
// title/description form + create mutation (reuse, no duplicated create logic).
export function OrgWorkItemCreateModal({ kind, onClose }: Props): React.ReactElement {
  const { t } = useTranslation('work');
  const projects = useProjects();
  const [projectId, setProjectId] = useState('');
  // a11y: Escape closes + focus-trap on the project-picker phase. Active only
  // while the picker is shown; once delegated, the per-project modal owns it.
  const containerRef = useModalA11y({ open: !projectId, onClose });

  // Once a project is chosen, hand off to the existing per-project create modal.
  if (projectId) {
    return kind === 'issue' ? (
      <IssueCreateModal projectId={projectId} onClose={onClose} />
    ) : (
      <TaskCreateModal projectId={projectId} onClose={onClose} />
    );
  }

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="org-create-modal"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{t('workItem.create.title', { type: kind === 'issue' ? t('type.issue') : t('type.task') })}</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-text-muted hover:text-text-primary"
            aria-label={t('workItem.create.close')}
            data-testid="org-create-close"
          >
            {t('workItem.create.closeGlyph')}
          </button>
        </div>
        <label className="block text-sm">
          <span className="mb-1 block text-text-secondary">{t('workItem.create.project')}</span>
          <select
            data-testid="org-create-project-select"
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
            className="w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm"
          >
            <option value="">{t('workItem.create.selectProject')}</option>
            {projects.data?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <p className="mt-2 text-xs text-text-muted">
          {/* pass the TRANSLATED kind label so zh renders 议题/任务, not raw en 'issue'/'task' */}
          {t('workItem.create.hint', { kind: kind === 'issue' ? t('type.issue') : t('type.task') })}
        </p>
      </div>
    </div>
  );
}
