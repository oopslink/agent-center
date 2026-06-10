import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { uploadMessageAttachment, useSendMessage } from '@/api/conversations';
import { useMentionAutocomplete } from './useMentionAutocomplete';
import { MentionPicker } from './MentionPicker';

interface Props {
  conversationId: string;
}

// MessageComposer — single-line textarea + Send button. Per F6 oversight
// #3: Enter sends; Shift+Enter inserts a newline; submit is disabled
// while the mutation is pending; clears on success.
//
// Owns its own draft state (component-local — not server, not Zustand).
export function MessageComposer({ conversationId }: Props): React.ReactElement {
  const [draft, setDraft] = useState('');
  const [files, setFiles] = useState<Array<{ file: File; previewUrl: string | null }>>([]);
  // v2.7.1 #222: track IME composition so Enter that confirms a composition
  // (e.g. Chinese/Japanese input) doesn't fire send.
  const composingRef = useRef(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const send = useSendMessage();
  // v2.8 #275: #/@ mention picker wired to the textarea.
  const mention = useMentionAutocomplete({ setValue: setDraft, textareaRef });
  const disabled = (!draft.trim() && files.length === 0) || send.isPending;

  // Revoke any outstanding object URLs only on unmount. Per-item revokes happen
  // explicitly on Remove and after submit; a [files]-deps effect would instead
  // revoke still-displayed previews every time the list changes. A ref tracks
  // the latest list so the unmount cleanup sees current URLs without re-running.
  const filesRef = useRef(files);
  filesRef.current = files;
  useEffect(() => {
    return () => {
      for (const f of filesRef.current) {
        if (f.previewUrl) URL.revokeObjectURL(f.previewUrl);
      }
    };
  }, []);

  const submit = async () => {
    if (disabled) return;
    const content = draft.trim();
    try {
      const attachments = await Promise.all(files.map(({ file }) => uploadMessageAttachment(file)));
      await send.mutateAsync({ conversationId, content, attachments });
      setDraft('');
      setFiles((prev) => {
        for (const f of prev) {
          if (f.previewUrl) URL.revokeObjectURL(f.previewUrl);
        }
        return [];
      });
    } catch {
      // Error surfaces in send.error; leave draft intact so user can retry.
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

  return (
    <form
      className="flex items-center gap-2 border-t border-border-base bg-bg-elevated p-3"
      data-testid="message-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
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
        className="flex h-10 w-10 shrink-0 cursor-pointer items-center justify-center rounded border border-border-strong text-text-primary hover:bg-bg-subtle"
        title="Attach file"
        aria-label="Attach file"
        data-testid="composer-attach"
      >
        <PaperclipIcon />
        <input
          type="file"
          className="sr-only"
          data-testid="composer-file"
          onChange={(e) => {
            const picked = Array.from(e.currentTarget.files ?? []);
            setFiles((prev) => [
              ...prev,
              ...picked.map((file) => ({
                file,
                previewUrl: file.type.startsWith('image/') ? URL.createObjectURL(file) : null,
              })),
            ]);
            e.currentTarget.value = '';
          }}
          disabled={send.isPending}
        />
      </label>
      <button
        type="submit"
        disabled={disabled}
        className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-text-primary text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
        data-testid="composer-send"
        title="Send (Enter)"
        aria-label="Send"
        aria-busy={send.isPending}
      >
        <SendIcon />
      </button>
      {send.isError && (
        <span className="text-xs text-danger" data-testid="composer-error">
          {(send.error as Error).message}
        </span>
      )}
      {files.length > 0 && (
        <ul className="flex max-w-xs flex-wrap gap-2" data-testid="composer-attachments">
          {files.map(({ file, previewUrl }, idx) => (
            <li
              key={`${file.name}-${idx}`}
              className="flex items-center gap-2 rounded border border-border-base px-2 py-1 text-xs"
            >
              {previewUrl && (
                <img
                  src={previewUrl}
                  alt={file.name}
                  className="h-8 w-8 rounded object-cover"
                  data-testid="composer-attachment-preview"
                />
              )}
              <span className="max-w-32 truncate">{file.name}</span>
              <button
                type="button"
                className="text-text-muted hover:text-text-primary"
                aria-label={`Remove ${file.name}`}
                onClick={() => {
                  setFiles((prev) => {
                    const item = prev[idx];
                    if (item?.previewUrl) URL.revokeObjectURL(item.previewUrl);
                    return prev.filter((_, i) => i !== idx);
                  });
                }}
              >
                Remove
              </button>
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
