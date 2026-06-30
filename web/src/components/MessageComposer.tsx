import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { uploadMessageAttachment, useSendMessage } from '@/api/conversations';
import type { MessageAttachment } from '@/api/types';
import { useMentionAutocomplete } from './useMentionAutocomplete';
import { MentionPicker } from './MentionPicker';
import {
  formatBytes,
  isPreviewableImage,
  validateAttachmentFile,
} from './attachmentValidation';

interface Props {
  conversationId: string;
  // v2.9.1 Threads: when set, this composer sends REPLIES into the given root
  // message's thread (the POST carries parent_message_id). Used by ThreadSidebar;
  // absent on the main conversation composer (normal top-level send).
  parentMessageId?: string;
}

// v2.10.2 [T148]: the textarea auto-grows with its content up to this many rows;
// past it the field scrolls internally instead of growing further.
const MAX_COMPOSER_ROWS = 4;

// One staged attachment, from selection through upload. `status` drives the
// chip UI: ready (queued) → uploading (progress bar) → uploaded (has result) or
// error (retry button). `uploaded` is cached so a retry of a sibling never
// re-uploads an already-finished file.
interface StagedAttachment {
  id: string;
  file: File;
  previewUrl: string | null;
  status: 'ready' | 'uploading' | 'uploaded' | 'error';
  progress: number; // 0..100
  errorMsg?: string;
  uploaded?: MessageAttachment;
}

// MessageComposer — single-line textarea + Send button. Per F6 oversight
// #3: Enter sends; Shift+Enter inserts a newline; submit is disabled
// while the mutation is pending; clears on success.
//
// v2.9.2 polish: drag-and-drop + clipboard-image paste add attachments;
// per-file upload progress + retry; client-side size validation with inline
// rejection notices. Owns its own draft + attachment state (component-local —
// not server, not Zustand).
export function MessageComposer({ conversationId, parentMessageId }: Props): React.ReactElement {
  const { t } = useTranslation('chat');
  const [draft, setDraft] = useState('');
  const [attachments, setAttachments] = useState<StagedAttachment[]>([]);
  // Files rejected by the client-side gate (oversize/empty) from the most
  // recent add — shown until the next add or a successful send replaces them.
  const [rejections, setRejections] = useState<Array<{ name: string; reason: string }>>([]);
  const [dragActive, setDragActive] = useState(false);
  // dragenter/dragleave fire for every child element; a depth counter keeps the
  // drop overlay stable instead of flickering as the cursor crosses children.
  const dragDepth = useRef(0);
  const idSeq = useRef(0);
  // v2.7.1 #222: track IME composition so Enter that confirms a composition
  // (e.g. Chinese/Japanese input) doesn't fire send.
  const composingRef = useRef(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const send = useSendMessage();
  // v2.8 #275: #/@ mention picker wired to the textarea.
  const mention = useMentionAutocomplete({ setValue: setDraft, textareaRef });

  const uploading = attachments.some((a) => a.status === 'uploading');
  const disabled =
    (!draft.trim() && attachments.length === 0) || send.isPending || uploading;

  // Revoke any outstanding object URLs only on unmount. Per-item revokes happen
  // explicitly on Remove and after submit; an [attachments]-deps effect would
  // instead revoke still-displayed previews every time the list changes. A ref
  // tracks the latest list so the unmount cleanup sees current URLs without
  // re-running.
  const attachmentsRef = useRef(attachments);
  attachmentsRef.current = attachments;
  useEffect(() => {
    return () => {
      for (const a of attachmentsRef.current) {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      }
    };
  }, []);

  // v2.10.2 [T148]: auto-grow the textarea with its content, capped at
  // MAX_COMPOSER_ROWS lines — beyond that it scrolls internally instead of pushing
  // the composer taller. Recomputes on every draft change (incl. the clear-on-send,
  // which collapses it back to one line). Measures the live line-height/padding/
  // border so it stays correct if the type scale changes.
  useEffect(() => {
    const ta = textareaRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    const cs = window.getComputedStyle(ta);
    const line = parseFloat(cs.lineHeight) || 20;
    const padY = (parseFloat(cs.paddingTop) || 0) + (parseFloat(cs.paddingBottom) || 0);
    const borderY = (parseFloat(cs.borderTopWidth) || 0) + (parseFloat(cs.borderBottomWidth) || 0);
    const maxH = line * MAX_COMPOSER_ROWS + padY + borderY;
    const full = ta.scrollHeight + borderY; // border-box: scrollHeight omits the border
    ta.style.height = `${Math.min(full, maxH)}px`;
    ta.style.overflowY = full > maxH ? 'auto' : 'hidden';
  }, [draft]);

  // patchAttachment — immutable update of one staged item by id.
  const patchAttachment = (id: string, patch: Partial<StagedAttachment>) => {
    setAttachments((prev) => prev.map((a) => (a.id === id ? { ...a, ...patch } : a)));
  };

  // addFiles validates each picked/dropped/pasted file: oversize/empty ones go
  // to the rejection notice; the rest are staged with an image preview when
  // applicable. Replaces the rejection list with this batch's rejects.
  const addFiles = (fileList: FileList | File[] | null) => {
    const picked = Array.from(fileList ?? []);
    if (picked.length === 0) return;
    const staged: StagedAttachment[] = [];
    const rejected: Array<{ name: string; reason: string }> = [];
    for (const file of picked) {
      const reason = validateAttachmentFile(file);
      if (reason) {
        rejected.push({ name: file.name, reason });
        continue;
      }
      staged.push({
        id: `att-${idSeq.current++}`,
        file,
        previewUrl: isPreviewableImage(file.type) ? URL.createObjectURL(file) : null,
        status: 'ready',
        progress: 0,
      });
    }
    if (staged.length > 0) setAttachments((prev) => [...prev, ...staged]);
    setRejections(rejected);
  };

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const item = prev.find((a) => a.id === id);
      if (item?.previewUrl) URL.revokeObjectURL(item.previewUrl);
      return prev.filter((a) => a.id !== id);
    });
  };

  // openPreview — open a staged attachment in a new tab (images / PDF preview
  // inline, other types download). Reuses the cached image previewUrl when
  // present; for non-image files mints a short-lived object URL.
  const openPreview = (item: StagedAttachment) => {
    const url = item.previewUrl ?? URL.createObjectURL(item.file);
    window.open(url, '_blank', 'noopener,noreferrer');
    if (!item.previewUrl) window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
  };

  const clearAttachments = () => {
    setAttachments((prev) => {
      for (const a of prev) {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      }
      return [];
    });
    setRejections([]);
  };

  // uploadOne runs (or retries) the upload for a single staged item, streaming
  // progress into its chip. Returns the uploaded attachment, or null on failure
  // (the item is left in `error` so the user can retry).
  const uploadOne = async (item: StagedAttachment): Promise<MessageAttachment | null> => {
    patchAttachment(item.id, { status: 'uploading', progress: 0, errorMsg: undefined });
    try {
      const result = await uploadMessageAttachment(item.file, {
        onProgress: ({ loaded, total }) =>
          patchAttachment(item.id, {
            progress: total > 0 ? Math.round((loaded / total) * 100) : 0,
          }),
      });
      patchAttachment(item.id, { status: 'uploaded', progress: 100, uploaded: result });
      return result;
    } catch (err) {
      patchAttachment(item.id, {
        status: 'error',
        errorMsg: err instanceof Error ? err.message : t('composer.uploadFailed'),
      });
      return null;
    }
  };

  // Pre-upload: start uploading each file the moment it's staged, so it's already
  // on the server by the time the user hits send (submit then just reuses the
  // cached result). uploadOne flips the item to 'uploading' synchronously, so
  // each 'ready' item is picked up exactly once.
  useEffect(() => {
    for (const a of attachments) {
      if (a.status === 'ready') void uploadOne(a);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachments]);

  const submit = async () => {
    if (disabled) return;
    const content = draft.trim();
    const snapshot = attachmentsRef.current;
    // Upload everything not already finished (covers first send + retry).
    const pending = snapshot.filter((a) => a.status !== 'uploaded');
    const results = await Promise.all(pending.map(uploadOne));
    if (results.some((r) => r === null)) return; // a file failed — leave for retry

    // Reassemble the final list in original order: cached results for already-
    // uploaded items, fresh results for the ones we just uploaded.
    const byId = new Map<string, MessageAttachment>();
    snapshot.forEach((a) => {
      if (a.status === 'uploaded' && a.uploaded) byId.set(a.id, a.uploaded);
    });
    pending.forEach((a, i) => {
      const r = results[i];
      if (r) byId.set(a.id, r);
    });
    const finalAttachments = snapshot
      .map((a) => byId.get(a.id))
      .filter((a): a is MessageAttachment => a != null);

    try {
      await send.mutateAsync({
        conversationId,
        content,
        attachments: finalAttachments,
        // Only attach parent_message_id when this is a thread composer — a
        // top-level composer leaves it undefined (omitted from the POST body).
        ...(parentMessageId ? { parent_message_id: parentMessageId } : {}),
      });
      setDraft('');
      clearAttachments();
    } catch {
      // Error surfaces in send.error; leave draft + (now-uploaded) attachments
      // intact so the user can retry the send without re-uploading.
    }
  };

  const handleKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // v2.8 #275: while the mention picker is open it owns ↑↓/Enter/Tab/Esc.
    if (mention.onKeyDown(e)) return;
    if (e.key === 'Enter' && !e.shiftKey) {
      // v2.7.1 #222: while an IME composition is active, Enter confirms the
      // candidate — never send. Shift+Enter (newline) is unaffected.
      if (composingRef.current || e.nativeEvent.isComposing) return;
      e.preventDefault();
      void submit();
    }
  };

  // v2.9.2: paste clipboard files (e.g. a screenshot) straight into the
  // attachment list. Only consume the event when files are present so normal
  // text paste is untouched.
  const handlePaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = e.clipboardData?.files;
    if (files && files.length > 0) {
      e.preventDefault();
      addFiles(files);
    }
  };

  // v2.9.2: drag-and-drop onto the composer. dragenter/leave use a depth
  // counter (children re-fire the events); dragover must preventDefault to mark
  // the form a valid drop target.
  const handleDragEnter = (e: React.DragEvent) => {
    if (!Array.from(e.dataTransfer?.types ?? []).includes('Files')) return;
    e.preventDefault();
    dragDepth.current += 1;
    setDragActive(true);
  };
  const handleDragOver = (e: React.DragEvent) => {
    if (!Array.from(e.dataTransfer?.types ?? []).includes('Files')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
  };
  const handleDragLeave = (e: React.DragEvent) => {
    if (dragDepth.current === 0) return;
    e.preventDefault();
    dragDepth.current -= 1;
    if (dragDepth.current <= 0) {
      dragDepth.current = 0;
      setDragActive(false);
    }
  };
  const handleDrop = (e: React.DragEvent) => {
    if (!Array.from(e.dataTransfer?.types ?? []).includes('Files')) return;
    e.preventDefault();
    dragDepth.current = 0;
    setDragActive(false);
    addFiles(e.dataTransfer.files);
  };

  return (
    <form
      // v2.10.2 [T148]: a vertical stack — the (auto-growing) textarea on top, the
      // action buttons in a bar at the BOTTOM (was a single inline row).
      className="relative flex flex-col gap-2 border-t border-border-base bg-bg-elevated p-3 md:px-6 md:py-4"
      data-testid="message-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {dragActive && (
        <div
          className="pointer-events-none absolute inset-1 z-20 flex items-center justify-center rounded border-2 border-dashed border-accent bg-bg-subtle/90 text-sm font-medium text-text-primary"
          data-testid="composer-dropzone"
        >
          {t('composer.dropToAttach')}
        </div>
      )}
      <div className="relative">
        {mention.open && (
          <div className="absolute bottom-full left-0 z-10 mb-1 w-72" data-testid="mention-popup">
            <MentionPicker
              options={mention.options}
              activeId={mention.activeId}
              listboxId={mention.listboxId}
              onSelect={mention.onSelect}
              onHoverActivate={mention.onHoverActivate}
            />
          </div>
        )}
        <textarea
          ref={textareaRef}
          // v2.10.2 [T148]: leading-5 + py-1.5 give a 1-line start; the auto-grow
          // effect sets the height (capped at MAX_COMPOSER_ROWS) and toggles
          // overflow-y, so a long draft scrolls internally instead of growing past
          // 4 lines.
          className="block w-full resize-none rounded border border-border-strong bg-bg-elevated px-3 py-1.5 text-sm leading-5 text-text-primary placeholder:text-text-muted focus:border-accent"
          rows={1}
          aria-label={t('composer.messageLabel')}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={mention.open}
          aria-controls={mention.open ? mention.listboxId : undefined}
          aria-activedescendant={mention.activeOptionId}
          placeholder={t('composer.placeholder')}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            mention.sync();
          }}
          onKeyDown={handleKey}
          onKeyUp={() => mention.sync()}
          onClick={() => mention.sync()}
          onPaste={handlePaste}
          onCompositionStart={() => {
            composingRef.current = true;
          }}
          onCompositionEnd={() => {
            composingRef.current = false;
          }}
          data-testid="composer-textarea"
          disabled={send.isPending}
        />
      </div>
      {send.isError && (
        <span className="text-xs text-danger" data-testid="composer-error">
          {(send.error as Error).message}
        </span>
      )}
      {rejections.length > 0 && (
        <ul className="basis-full text-xs text-danger" data-testid="composer-rejections">
          {rejections.map((r, i) => (
            <li key={`${r.name}-${i}`} data-testid="composer-rejection">
              {t('composer.rejection', { name: r.name, reason: r.reason })}
            </li>
          ))}
        </ul>
      )}
      {attachments.length > 0 && (
        <ul className="flex max-w-xs max-md:max-w-full flex-wrap max-md:flex-col gap-2" data-testid="composer-attachments">
          {attachments.map((a) => (
            <li
              key={a.id}
              className="flex w-44 max-md:w-full flex-col gap-1 rounded border border-border-base px-2 py-1 text-xs"
              data-testid="composer-attachment"
            >
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-2 text-left hover:opacity-80"
                  title={t('composer.previewFile', { name: a.file.name })}
                  onClick={() => openPreview(a)}
                  data-testid="composer-attachment-open"
                >
                  {a.previewUrl && (
                    <img
                      src={a.previewUrl}
                      alt={a.file.name}
                      className="h-8 w-8 max-md:h-12 max-md:w-12 shrink-0 rounded object-cover"
                      data-testid="composer-attachment-preview"
                    />
                  )}
                  <span className="min-w-0 flex-1 truncate">{a.file.name}</span>
                </button>
                <span className="shrink-0 text-text-muted">{formatBytes(a.file.size)}</span>
                <button
                  type="button"
                  className="shrink-0 rounded px-1 text-base leading-none text-text-muted hover:text-text-primary"
                  aria-label={t('composer.removeFile', { name: a.file.name })}
                  onClick={() => removeAttachment(a.id)}
                >
                  ×
                </button>
              </div>
              {a.status === 'uploading' && (
                <div
                  className="h-1 w-full overflow-hidden rounded bg-bg-subtle"
                  role="progressbar"
                  aria-valuenow={a.progress}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label={t('composer.uploadingFile', { name: a.file.name })}
                  data-testid="composer-attachment-progress"
                >
                  <div className="h-full bg-accent" style={{ width: `${a.progress}%` }} />
                </div>
              )}
              {a.status === 'error' && (
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-danger" data-testid="composer-attachment-error">
                    {a.errorMsg}
                  </span>
                  <button
                    type="button"
                    className="shrink-0 text-accent hover:underline"
                    data-testid="composer-attachment-retry"
                    onClick={() => void uploadOne(a)}
                  >
                    {t('composer.retry')}
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      {/* v2.10.2 [T148]: action bar at the BOTTOM of the composer — attach on the
          left, send on the right — one size smaller than the prior inline buttons
          (h-8 w-8, was h-11/md:h-10). Owner-directed (T148); supersedes the M2 44px
          touch sizing for these two icon controls. */}
      <div className="flex items-center justify-between gap-2">
        <label
          className="flex h-11 w-11 md:h-8 md:w-8 shrink-0 cursor-pointer items-center justify-center rounded border border-border-strong text-text-primary hover:bg-bg-subtle"
          title={t('composer.attachFile')}
          aria-label={t('composer.attachFile')}
          data-testid="composer-attach"
        >
          <PaperclipIcon />
          <input
            type="file"
            multiple
            className="sr-only"
            data-testid="composer-file"
            onChange={(e) => {
              addFiles(e.currentTarget.files);
              e.currentTarget.value = '';
            }}
            disabled={send.isPending}
          />
        </label>
        <button
          type="submit"
          disabled={disabled}
          className="flex h-11 w-11 md:h-8 md:w-8 shrink-0 items-center justify-center rounded bg-btn-primary-bg text-btn-primary-fg hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
          data-testid="composer-send"
          title={t('composer.sendTitle')}
          aria-label={t('composer.send')}
          aria-busy={send.isPending || uploading}
        >
          <SendIcon />
        </button>
      </div>
    </form>
  );
}

// v2.7.1 #222: inline icons (no-emoji UX rule — single-stroke 20×20 SVGs).
function PaperclipIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path
        d="M14.5 9.5l-4.8 4.8a3 3 0 0 1-4.2-4.2l5.5-5.5a2 2 0 0 1 2.8 2.8L8.3 12.7a1 1 0 0 1-1.4-1.4l4.6-4.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SendIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 10l14-6-6 14-2.5-5.5L3 10z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
