import { describe, expect, it } from 'vitest';
import {
  MAX_ATTACHMENT_BYTES,
  formatBytes,
  isPreviewableImage,
  validateAttachmentFile,
} from './attachmentValidation';

describe('attachmentValidation', () => {
  describe('formatBytes', () => {
    it('renders bytes / KB / MB with one decimal under 10 of a unit', () => {
      expect(formatBytes(0)).toBe('0 B');
      expect(formatBytes(512)).toBe('512 B');
      expect(formatBytes(1024)).toBe('1.0 KB');
      expect(formatBytes(1536)).toBe('1.5 KB');
      expect(formatBytes(20 * 1024)).toBe('20 KB');
      expect(formatBytes(1024 * 1024)).toBe('1.0 MB');
      expect(formatBytes(MAX_ATTACHMENT_BYTES)).toBe('25 MB');
    });
  });

  describe('isPreviewableImage', () => {
    it('is true for any image/* and false otherwise', () => {
      expect(isPreviewableImage('image/png')).toBe(true);
      expect(isPreviewableImage('image/jpeg')).toBe(true);
      expect(isPreviewableImage('image/webp')).toBe(true);
      expect(isPreviewableImage('application/pdf')).toBe(false);
      expect(isPreviewableImage('')).toBe(false);
    });
  });

  describe('validateAttachmentFile', () => {
    it('accepts any in-range non-empty file regardless of type', () => {
      expect(validateAttachmentFile({ size: 1 })).toBeNull();
      expect(validateAttachmentFile({ size: MAX_ATTACHMENT_BYTES })).toBeNull();
    });

    it('rejects an empty file', () => {
      expect(validateAttachmentFile({ size: 0 })).toBe('is empty');
    });

    it('rejects a file over the 25 MB cap with the limit in the reason', () => {
      const reason = validateAttachmentFile({ size: MAX_ATTACHMENT_BYTES + 1 });
      expect(reason).toContain('25 MB');
      expect(reason).toContain('exceeds');
    });
  });
});
