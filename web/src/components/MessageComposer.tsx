import type React from 'react';
import { useEffect, useRef, useState } from 'react';
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
        errorMsg: err instanceof Error ? err.message : 'upload failed',
      });
      return null;
    }
  };

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
      className="relative flex items-center gap-2 border-t border-border-base bg-bg-elevated p-3"
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
          Drop files to attach
        </div>
      )}
      <div className="relative flex-1">
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
          className="h-10 w-full resize-none rounded border border-border-strong bg-bg-elevated px-3 py-0 text-sm leading-10 text-text-primary placeholder:text-text-muted focus:border-accent"
          rows={1}
          aria-label="Message"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={mention.open}
          aria-controls={mention.open ? mention.listboxId : undefined}
          aria-activedescendant={mention.activeOptionId}
          placeholder="Type a message — Enter to send, Shift+Enter for newline"
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
      <label
        className="flex h-11 w-11 shrink-0 cursor-pointer items-center justify-center rounded border border-border-strong text-text-primary hover:bg-bg-subtle md:h-10 md:w-10"
        title="Attach file"
        aria-label="Attach file"
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
        className="flex h-11 w-11 shrink-0 items-center justify-center rounded bg-text-primary text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted md:h-10 md:w-10"
        data-testid="composer-send"
        title="Send (Enter)"
        aria-label="Send"
        aria-busy={send.isPending || uploading}
      >
        <SendIcon />
      </button>
      {send.isError && (
        <span className="text-xs text-danger" data-testid="composer-error">
          {(send.error as Error).message}
        </span>
      )}
      {rejections.length > 0 && (
        <ul className="basis-full text-xs text-danger" data-testid="composer-rejections">
          {rejections.map((r, i) => (
            <li key={`${r.name}-${i}`} data-testid="composer-rejection">
              {r.name} — {r.reason}
            </li>
          ))}
        </ul>
      )}
      {attachments.length > 0 && (
        <ul className="flex max-w-xs flex-wrap gap-2" data-testid="composer-attachments">
          {attachments.map((a) => (
            <li
              key={a.id}
              className="flex w-44 flex-col gap-1 rounded border border-border-base px-2 py-1 text-xs"
              data-testid="composer-attachment"
            >
              <div className="flex items-center gap-2">
                {a.previewUrl && (
                  <img
                    src={a.previewUrl}
                    alt={a.file.name}
                    className="h-8 w-8 shrink-0 rounded object-cover"
                    data-testid="composer-attachment-preview"
                  />
                )}
                <span className="min-w-0 flex-1 truncate" title={a.file.name}>
                  {a.file.name}
                </span>
                <span className="shrink-0 text-text-muted">{formatBytes(a.file.size)}</span>
                <button
                  type="button"
                  className="shrink-0 text-text-muted hover:text-text-primary disabled:opacity-50"
                  aria-label={`Remove ${a.file.name}`}
                  disabled={a.status === 'uploading'}
                  onClick={() => removeAttachment(a.id)}
                >
                  Remove
                </button>
              </div>
              {a.status === 'uploading' && (
                <div
                  className="h-1 w-full overflow-hidden rounded bg-bg-subtle"
                  role="progressbar"
                  aria-valuenow={a.progress}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label={`Uploading ${a.file.name}`}
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
                    Retry
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </form>
  );
}

// v2.7.1 #222: inline icons (no-emoji UX rule — single-stroke 20×20 SVGs).
function PaperclipIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
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
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 10l14-6-6 14-2.5-5.5L3 10z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
