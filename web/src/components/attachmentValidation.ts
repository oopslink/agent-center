// Attachment client-side validation + display helpers (v2.9.2 composer polish).
//
// Product defaults (owner 2026-06-14, adjustable): images png/jpg/jpeg/gif/webp
// render inline; ANY file type is accepted; per-file cap 25 MB; multiple
// attachments per message. The only hard reject gate is size (and empty files);
// type is NOT a reject reason — it only drives whether we render an inline
// preview. Keep these constants here so the gate is one source of truth and the
// composer + tests + a future server check can share them.

export const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024; // 25 MB per file

// Mime types we render as an inline image preview (png/jpg/jpeg/gif/webp).
// jpg and jpeg both surface as image/jpeg, so four entries cover all five
// extensions. Any other type is still accepted — it just shows as a file chip.
export const PREVIEW_IMAGE_MIME_TYPES = [
  'image/png',
  'image/jpeg',
  'image/gif',
  'image/webp',
] as const;

// isPreviewableImage — true when the file should get an inline thumbnail. We
// accept any `image/*` (browsers can render more than the canonical five), but
// fall back to a file chip for everything else.
export function isPreviewableImage(mimeType: string): boolean {
  return mimeType.startsWith('image/');
}

// formatBytes — compact human size for chips + limit messages (e.g. "25 MB",
// "1.4 MB", "812 KB", "0 B"). One decimal under 10 of a unit, none at/above.
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`;
  const mb = kb / 1024;
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`;
}

// validateAttachmentFile — null when the file may be attached, otherwise a
// short reason phrase (no filename) suitable for "<name> — <reason>". Only size
// is gated: empty files are rejected (nothing to upload) and anything over the
// 25 MB cap is rejected; every type is allowed.
export function validateAttachmentFile(file: { size: number }): string | null {
  if (file.size === 0) return 'is empty';
  if (file.size > MAX_ATTACHMENT_BYTES) {
    return `exceeds the ${formatBytes(MAX_ATTACHMENT_BYTES)} limit`;
  }
  return null;
}
