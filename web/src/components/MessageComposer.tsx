import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { uploadMessageAttachment, useSendMessage } from '@/api/conversations';

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
  const send = useSendMessage();
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
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <form
      className="flex items-end gap-2 border-t border-border-base bg-bg-elevated p-3"
      data-testid="message-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <textarea
        className="min-h-[2.5rem] flex-1 resize-none rounded border border-border-strong bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
        rows={1}
        aria-label="Message"
        placeholder="Type a message — Enter to send, Shift+Enter for newline"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={handleKey}
        data-testid="composer-textarea"
        disabled={send.isPending}
      />
      <label
        className="rounded border border-border-strong px-3 py-2 text-sm text-text-primary hover:bg-bg-subtle"
        title="Attach file"
      >
        Attach
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
        className="rounded bg-text-primary px-4 py-2 text-sm font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
        data-testid="composer-send"
      >
        {send.isPending ? 'Sending…' : 'Send'}
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
