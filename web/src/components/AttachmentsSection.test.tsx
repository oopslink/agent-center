// v2.10.0 [T73]: Task/Issue attachments section — list, empty state, upload.
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { TaskAttachments } from './AttachmentsSection';

function renderTaskAttachments() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <TaskAttachments projectId="proj-1" taskId="task-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TaskAttachments', () => {
  afterEach(() => cleanup());

  it('lists task files with filename, size, and a download link', async () => {
    server.use(
      http.get('*/projects/proj-1/tasks/task-1/files', () =>
        HttpResponse.json({
          files: [
            { uri: 'ac://files/AAA111', filename: 'spec.pdf', mime_type: 'application/pdf', size: 2048, created_by: 'user:x', created_at: '2026-06-14T00:00:00Z' },
            { uri: 'ac://files/BBB222', filename: 'diagram.png', mime_type: 'image/png', size: 4096, created_by: 'user:x', created_at: '2026-06-14T00:00:00Z' },
          ],
        }),
      ),
    );
    renderTaskAttachments();
    await waitFor(() => expect(screen.getByTestId('attachments-list')).toBeInTheDocument());
    const list = screen.getByTestId('attachments-list');
    expect(list.textContent).toContain('spec.pdf');
    expect(list.textContent).toContain('diagram.png');
    expect(screen.getByTestId('attachments-count')).toHaveTextContent('2');
    // download link points at the generic file route by ulid
    const links = within(list).getAllByTestId('attachment-item');
    expect(links[0]).toHaveAttribute('href', expect.stringContaining('AAA111'));
  });

  it('shows an empty state when the task has no files', async () => {
    server.use(
      http.get('*/projects/proj-1/tasks/task-1/files', () => HttpResponse.json({ files: [] })),
    );
    renderTaskAttachments();
    await waitFor(() => expect(screen.getByTestId('attachments-empty')).toBeInTheDocument());
  });

  it('uploads a picked file (create → put → complete) and refreshes the list', async () => {
    let uploaded = false;
    server.use(
      http.get('*/projects/proj-1/tasks/task-1/files', () =>
        HttpResponse.json({
          files: uploaded
            ? [{ uri: 'ac://files/NEW999', filename: 'added.txt', mime_type: 'text/plain', size: 5, created_by: 'user:x', created_at: '2026-06-14T00:00:00Z' }]
            : [],
        }),
      ),
      http.post('*/projects/proj-1/tasks/task-1/files', () =>
        HttpResponse.json({ file_uri: 'ac://files/NEW999', transfer_uri: 'ac://transfers/T1', transfer_id: 'T1' }, { status: 201 }),
      ),
      http.put('*/files/transfer/T1', () => HttpResponse.json({ written: true })),
      http.post('*/projects/proj-1/tasks/task-1/files/transfer/T1/complete', () => {
        uploaded = true;
        return HttpResponse.json({ completed: true, file_uri: 'ac://files/NEW999' });
      }),
    );
    renderTaskAttachments();
    await waitFor(() => expect(screen.getByTestId('attachments-empty')).toBeInTheDocument());

    const input = screen.getByTestId('attachments-file-input') as HTMLInputElement;
    const file = new File(['hello'], 'added.txt', { type: 'text/plain' });
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => expect(screen.getByTestId('attachments-list')).toBeInTheDocument());
    expect(screen.getByTestId('attachments-list').textContent).toContain('added.txt');
  });
});
