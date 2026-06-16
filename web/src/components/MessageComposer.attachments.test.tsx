import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from 'vitest';
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MessageComposer } from './MessageComposer';
import { uploadMessageAttachment } from '@/api/conversations';
import { MAX_ATTACHMENT_BYTES } from './attachmentValidation';

// Mock only the upload helper; useSendMessage stays real (drives the message
// POST through MSW) so we exercise the genuine submit/clear/retry flow.
vi.mock('@/api/conversations', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/conversations')>();
  return { ...actual, uploadMessageAttachment: vi.fn() };
});

const mockUpload = uploadMessageAttachment as unknown as Mock;

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

// fileOf builds a File and overrides its reported size (jsdom derives size from
// content, so an oversize file would otherwise need megabytes of payload).
function fileOf(name: string, type: string, size = 4): File {
  const f = new File(['x'.repeat(Math.min(size, 4))], name, { type });
  Object.defineProperty(f, 'size', { value: size });
  return f;
}

function uploadedFor(file: File) {
  return {
    uri: `ac://files/${file.name}`,
    filename: file.name,
    mime_type: file.type || 'application/octet-stream',
    size: file.size,
  };
}

describe('MessageComposer attachments (v2.9.2)', () => {
  let lastBody: { content: string; attachments?: Array<{ filename: string }> } | null;

  beforeEach(() => {
    lastBody = null;
    mockUpload.mockReset();
    mockUpload.mockImplementation(async (file: File) => uploadedFor(file));
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        lastBody = (await request.json()) as typeof lastBody;
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
  });
  afterEach(() => cleanup());

  it('adds a dropped file as an attachment chip', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('a.txt', 'text/plain')], types: ['Files'] },
    });
    expect(screen.getByTestId('composer-attachment')).toBeInTheDocument();
    expect(screen.getByText('a.txt')).toBeInTheDocument();
    // v2.10.3 [T178]: let the pre-upload settle so the trailing state update stays inside act.
    await waitFor(() => expect(mockUpload).toHaveBeenCalled());
  });

  it('shows the drop overlay while a file drag is over the composer', () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    expect(screen.queryByTestId('composer-dropzone')).not.toBeInTheDocument();
    fireEvent.dragEnter(form, { dataTransfer: { files: [], types: ['Files'] } });
    expect(screen.getByTestId('composer-dropzone')).toBeInTheDocument();
    fireEvent.dragLeave(form, { dataTransfer: { files: [], types: ['Files'] } });
    expect(screen.queryByTestId('composer-dropzone')).not.toBeInTheDocument();
  });

  it('pastes a clipboard image as an attachment', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea');
    fireEvent.paste(ta, {
      clipboardData: { files: [fileOf('shot.png', 'image/png')] },
    });
    expect(screen.getByTestId('composer-attachment')).toBeInTheDocument();
    expect(screen.getByTestId('composer-attachment-preview')).toBeInTheDocument();
    await waitFor(() => expect(mockUpload).toHaveBeenCalled());
  });

  it('rejects an oversize file with an inline notice and does not stage it', () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: {
        files: [fileOf('huge.bin', 'application/octet-stream', MAX_ATTACHMENT_BYTES + 1)],
        types: ['Files'],
      },
    });
    expect(screen.getByTestId('composer-rejection')).toHaveTextContent('25 MB');
    expect(screen.queryByTestId('composer-attachment')).not.toBeInTheDocument();
  });

  it('uploads the attachment then sends it with the message', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('doc.pdf', 'application/pdf')], types: ['Files'] },
    });
    await userEvent.type(screen.getByTestId('composer-textarea'), 'see attached');
    fireEvent.click(screen.getByTestId('composer-send'));

    await waitFor(() => expect(screen.queryByTestId('composer-attachment')).not.toBeInTheDocument());
    expect(mockUpload).toHaveBeenCalledTimes(1);
    expect(lastBody?.content).toBe('see attached');
    expect(lastBody?.attachments?.[0]?.filename).toBe('doc.pdf');
  });

  it('shows a progress bar and busy send while the upload is in flight', async () => {
    let resolveUpload: (() => void) | null = null;
    mockUpload.mockImplementation(
      (file: File, opts: { onProgress?: (p: { loaded: number; total: number }) => void }) =>
        new Promise((res) => {
          opts.onProgress?.({ loaded: 40, total: 100 });
          resolveUpload = () => res(uploadedFor(file));
        }),
    );
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    // v2.10.3 [T178]: pre-upload starts on drop — the progress bar and the
    // disabled (busy) Send appear without a Send click.
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('big.bin', 'application/octet-stream')], types: ['Files'] },
    });

    const bar = await screen.findByTestId('composer-attachment-progress');
    expect(bar).toHaveAttribute('aria-valuenow', '40');
    expect(screen.getByTestId('composer-send')).toBeDisabled();

    await act(async () => {
      resolveUpload?.();
    });
    // Upload done → Send re-enabled; clicking it posts and clears the chips.
    await waitFor(() => expect(screen.getByTestId('composer-send')).not.toBeDisabled());
    fireEvent.click(screen.getByTestId('composer-send'));
    await waitFor(() => expect(screen.queryByTestId('composer-attachment')).not.toBeInTheDocument());
  });

  it('keeps a failed upload for retry, then sends after the retry succeeds', async () => {
    mockUpload
      .mockRejectedValueOnce(new Error('upload failed: 500'))
      .mockImplementation(async (file: File) => uploadedFor(file));
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('r.txt', 'text/plain')], types: ['Files'] },
    });
    fireEvent.click(screen.getByTestId('composer-send'));

    // First upload fails → error + retry surface, no message posted.
    await screen.findByTestId('composer-attachment-error');
    expect(screen.getByTestId('composer-attachment-retry')).toBeInTheDocument();
    expect(lastBody).toBeNull();

    // Retry uploads successfully; sending now posts with the attachment.
    fireEvent.click(screen.getByTestId('composer-attachment-retry'));
    await waitFor(() =>
      expect(screen.queryByTestId('composer-attachment-error')).not.toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId('composer-send'));
    await waitFor(() => expect(lastBody?.attachments?.[0]?.filename).toBe('r.txt'));
  });

  // --- v2.10.3 [T178] composer attachment UX -------------------------------

  it('renders the remove control as an × button (no "Remove" text)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('a.txt', 'text/plain')], types: ['Files'] },
    });
    // The control keeps its accessible name but no longer shows the word "Remove".
    expect(screen.getByLabelText('Remove a.txt')).toBeInTheDocument();
    expect(screen.queryByText('Remove')).not.toBeInTheDocument();
    // Let the pre-upload settle so the trailing state update stays inside act.
    await waitFor(() => expect(mockUpload).toHaveBeenCalled());
  });

  it('pre-uploads each file the moment it is staged (no Send click needed)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const form = screen.getByTestId('message-composer');
    fireEvent.drop(form, {
      dataTransfer: { files: [fileOf('doc.pdf', 'application/pdf')], types: ['Files'] },
    });
    await waitFor(() => expect(mockUpload).toHaveBeenCalledTimes(1));
  });

  it('opens a preview in a new tab when the attachment chip is clicked', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null);
    try {
      wrap(<MessageComposer conversationId="C1" />);
      const ta = screen.getByTestId('composer-textarea');
      fireEvent.paste(ta, { clipboardData: { files: [fileOf('shot.png', 'image/png')] } });

      fireEvent.click(screen.getByTestId('composer-attachment-open'));
      expect(openSpy).toHaveBeenCalledTimes(1);
      expect(openSpy.mock.calls[0]?.[1]).toBe('_blank');
      await waitFor(() => expect(mockUpload).toHaveBeenCalled());
    } finally {
      openSpy.mockRestore();
    }
  });
});
