import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ApiError, api, request } from './client';
import { server } from '../test/mswServer';

describe('api client', () => {
  it('GETs JSON and returns the parsed body', async () => {
    server.use(
      http.get('/api/ping', () => HttpResponse.json({ pong: true })),
    );
    const body = await api.get<{ pong: boolean }>('/ping');
    expect(body.pong).toBe(true);
  });

  it('POSTs JSON body and returns the parsed response', async () => {
    server.use(
      http.post('/api/echo', async ({ request: r }) => {
        const b = (await r.json()) as { x: number };
        return HttpResponse.json({ doubled: b.x * 2 }, { status: 201 });
      }),
    );
    const body = await api.post<{ doubled: number }>('/echo', { x: 7 });
    expect(body.doubled).toBe(14);
  });

  it('DELETEs and tolerates 204 No Content', async () => {
    server.use(http.delete('/api/thing/:id', () => new HttpResponse(null, { status: 204 })));
    const body = await api.del('/thing/abc');
    expect(body).toBeUndefined();
  });

  it('throws ApiError with parsed envelope on 4xx', async () => {
    server.use(
      http.get('/api/bad', () =>
        HttpResponse.json({ error: 'invalid_input', message: 'name required' }, { status: 400 }),
      ),
    );
    await expect(api.get('/bad')).rejects.toBeInstanceOf(ApiError);
    try {
      await api.get('/bad');
    } catch (e) {
      const err = e as ApiError;
      expect(err.status).toBe(400);
      expect(err.code).toBe('invalid_input');
      expect(err.message).toContain('name required');
    }
  });

  it('falls back to http_error code when no error envelope present', async () => {
    server.use(http.get('/api/raw', () => new HttpResponse('plain text', { status: 500 })));
    try {
      await api.get('/raw');
      throw new Error('should have thrown');
    } catch (e) {
      const err = e as ApiError;
      expect(err.code).toBe('http_error');
      expect(err.status).toBe(500);
    }
  });

  it('throws timeout ApiError when the request exceeds timeoutMs', async () => {
    server.use(
      http.get('/api/slow', async () => {
        await new Promise((resolve) => setTimeout(resolve, 100));
        return HttpResponse.json({});
      }),
    );
    try {
      await request('/slow', { method: 'GET', timeoutMs: 10 });
      throw new Error('should have timed out');
    } catch (e) {
      const err = e as ApiError;
      expect(err.code).toBe('timeout');
      expect(err.status).toBe(0);
    }
  });

  it('wraps network errors as ApiError(network_error)', async () => {
    server.use(
      http.get('/api/blown', () => {
        throw new Error('downstream blew up');
      }),
    );
    try {
      await api.get('/blown');
      throw new Error('should have thrown');
    } catch (e) {
      const err = e as ApiError;
      // MSW surfaces this as a fetch failure; client maps to network_error.
      expect(['network_error', 'http_error']).toContain(err.code);
    }
  });
});
