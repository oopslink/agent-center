import type React from 'react';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useTaskFiles,
  useIssueFiles,
  useUploadTaskFile,
  useUploadIssueFile,
  type ScopeFile,
} from '@/api/files';
import { attachmentHref, attachmentKind } from '@/components/MessageList';
import {
  formatBytes,
  isPreviewableImage,
  validateAttachmentFile,
} from '@/components/attachmentValidation';

// v2.10.0 [T73]: the Attachments section for the Task / Issue detail pages —
// lists the task/issue-scoped files (preview + download) and uploads new ones.
// Presentational core (AttachmentsSection) + two thin hook-wiring wrappers
// (TaskAttachments / IssueAttachments) so both detail pages share one UI.

interface AttachmentsSectionProps {
  files: ScopeFile[];
  isLoading: boolean;
  isError: boolean;
  errorMessage?: string;
  uploading: boolean;
  uploadError?: string;
  onSelectFiles: (files: File[]) => void;
}

export function AttachmentsSection({
  files,
  isLoading,
  isError,
  errorMessage,
  uploading,
  uploadError,
  onSelectFiles,
}: AttachmentsSectionProps): React.ReactElement {
  const { t } = useTranslation('work');
  const inputRef = useRef<HTMLInputElement>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const handlePicked = (list: FileList | null) => {
    if (!list || list.length === 0) return;
    const picked = Array.from(list);
    for (const f of picked) {
      const err = validateAttachmentFile(f);
      if (err) {
        setLocalError(err);
        return;
      }
    }
    setLocalError(null);
    onSelectFiles(picked);
  };

  return (
    <section className="space-y-2" data-testid="attachments-section">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-text-primary">
          {t('widgets.attachments.title')}
          {files.length > 0 && (
            <span className="ml-1.5 text-xs font-normal text-text-muted" data-testid="attachments-count">
              {files.length}
            </span>
          )}
        </h2>
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          disabled={uploading}
          className="rounded border border-border-base px-2 py-1 text-xs font-medium text-text-primary hover:bg-bg-subtle disabled:opacity-50"
          data-testid="attachments-upload-btn"
        >
          {uploading ? t('widgets.attachments.uploading') : t('widgets.attachments.upload')}
        </button>
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          data-testid="attachments-file-input"
          onChange={(e) => {
            handlePicked(e.target.files);
            e.target.value = ''; // allow re-picking the same file
          }}
        />
      </div>

      {(localError || uploadError) && (
        <p className="text-xs text-danger" role="alert" data-testid="attachments-error">
          {localError ?? uploadError}
        </p>
      )}

      {isLoading && (
        <p className="text-xs text-text-muted" data-testid="attachments-loading">
          {t('widgets.attachments.loading')}
        </p>
      )}
      {isError && !isLoading && (
        <p className="text-xs text-danger" data-testid="attachments-load-error">
          {errorMessage ?? t('widgets.attachments.loadError')}
        </p>
      )}
      {!isLoading && !isError && files.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="attachments-empty">
          {t('widgets.attachments.empty')}
        </p>
      )}

      {files.length > 0 && (
        <ul className="flex flex-wrap gap-2" data-testid="attachments-list">
          {files.map((f) => (
            <li key={f.uri}>
              <a
                href={attachmentHref(f.uri)}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-2 rounded border border-border-base bg-bg-base px-2 py-1.5 text-xs hover:border-border-strong hover:bg-bg-subtle"
                data-testid="attachment-item"
                title={f.filename}
              >
                {isPreviewableImage(f.mime_type) ? (
                  <img
                    src={attachmentHref(f.uri)}
                    alt=""
                    className="h-10 w-10 rounded object-cover"
                  />
                ) : (
                  <span className="inline-flex h-10 w-10 items-center justify-center rounded bg-bg-elevated text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">
                    {attachmentKind(f.mime_type)}
                  </span>
                )}
                <span className="flex min-w-0 flex-col">
                  <span className="max-w-[12rem] truncate text-text-primary">{f.filename}</span>
                  <span className="text-text-muted">{formatBytes(f.size)}</span>
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// --- hook-wiring wrappers ----------------------------------------------------

function uploadEach(files: File[], upload: (f: File) => void): void {
  for (const f of files) upload(f);
}

export function TaskAttachments({
  projectId,
  taskId,
}: {
  projectId: string;
  taskId: string;
}): React.ReactElement {
  const list = useTaskFiles(projectId, taskId);
  const upload = useUploadTaskFile(projectId, taskId);
  return (
    <AttachmentsSection
      files={list.data ?? []}
      isLoading={list.isLoading}
      isError={list.isError}
      errorMessage={(list.error as Error | null)?.message}
      uploading={upload.isPending}
      uploadError={(upload.error as Error | null)?.message}
      onSelectFiles={(files) => uploadEach(files, upload.mutate)}
    />
  );
}

export function IssueAttachments({
  projectId,
  issueId,
}: {
  projectId: string;
  issueId: string;
}): React.ReactElement {
  const list = useIssueFiles(projectId, issueId);
  const upload = useUploadIssueFile(projectId, issueId);
  return (
    <AttachmentsSection
      files={list.data ?? []}
      isLoading={list.isLoading}
      isError={list.isError}
      errorMessage={(list.error as Error | null)?.message}
      uploading={upload.isPending}
      uploadError={(upload.error as Error | null)?.message}
      onSelectFiles={(files) => uploadEach(files, upload.mutate)}
    />
  );
}
