import { afterEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ApiError, api, request, withOrgSlug } from './client';
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
    // HttpResponse.error() makes fetch reject with a network-level failure
    // (no stderr noise — unlike throwing inside the handler, which MSW logs
    // as an unhandled exception).
    server.use(http.get('/api/blown', () => HttpResponse.error()));
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

describe('withOrgSlug — v2.9 path-routed org scope', () => {
  afterEach(() => {
    // reset jsdom URL back to the non-org-scoped default.
    window.history.pushState({}, '', '/');
  });

  function onOrgRoute(slug: string) {
    window.history.pushState({}, '', `/organizations/${slug}/projects`);
  }

  describe('no org slug in the browser URL', () => {
    it('returns /api-relative paths unchanged', () => {
      expect(withOrgSlug('/projects')).toBe('/projects');
    });
    it('returns full /api paths unchanged', () => {
      expect(withOrgSlug('/api/workers/w1')).toBe('/api/workers/w1');
    });
  });

  describe('on an org-scoped route', () => {
    it('splices /orgs/{slug} into an /api-relative resource path', () => {
      onOrgRoute('acme');
      expect(withOrgSlug('/projects')).toBe('/orgs/acme/projects');
    });

    it('preserves nested resource paths', () => {
      onOrgRoute('acme');
      expect(withOrgSlug('/projects/p1/plans')).toBe('/orgs/acme/projects/p1/plans');
    });

    it('preserves a trailing query string after the resource', () => {
      onOrgRoute('acme');
      expect(withOrgSlug('/issues?project=p1&status=open')).toBe(
        '/orgs/acme/issues?project=p1&status=open',
      );
    });

    it('inserts /orgs/{slug} AFTER the /api prefix for full-path raw fetches', () => {
      onOrgRoute('acme');
      expect(withOrgSlug('/api/workers/w1/name')).toBe('/api/orgs/acme/workers/w1/name');
    });

    it('uses the slug captured from the /organizations/:slug URL', () => {
      onOrgRoute('acme-corp');
      expect(withOrgSlug('/projects')).toBe('/orgs/acme-corp/projects');
    });

    it.each([
      ['/auth/login', '/auth/login'],
      ['/orgs', '/orgs'],
      ['/orgs/abc', '/orgs/abc'],
      ['/users/u1', '/users/u1'],
      ['/sse/subscribe', '/sse/subscribe'],
      ['/health', '/health'],
      ['/system/info', '/system/info'],
    ])('leaves exempt resource %s unchanged', (input, expected) => {
      onOrgRoute('acme');
      expect(withOrgSlug(input)).toBe(expected);
    });

    it.each([
      ['/api/auth/bootstrap', '/api/auth/bootstrap'],
      ['/api/orgs', '/api/orgs'],
      ['/api/orgs/abc', '/api/orgs/abc'],
      ['/api/users/u1', '/api/users/u1'],
      ['/api/sse', '/api/sse'],
    ])('leaves exempt full-path %s unchanged', (input, expected) => {
      onOrgRoute('acme');
      expect(withOrgSlug(input)).toBe(expected);
    });

    it('does not double-prefix an already org-scoped resource', () => {
      onOrgRoute('acme');
      expect(withOrgSlug('/orgs/acme/projects')).toBe('/orgs/acme/projects');
    });
  });

  it('request() fetches the path-routed URL for an org-scoped resource', async () => {
    window.history.pushState({}, '', '/organizations/acme/projects');
    let seen = '';
    server.use(
      http.get('/api/orgs/acme/projects', ({ request: r }) => {
        seen = new URL(r.url).pathname;
        return HttpResponse.json({ items: [] });
      }),
    );
    await api.get('/projects');
    expect(seen).toBe('/api/orgs/acme/projects');
    window.history.pushState({}, '', '/');
  });
});
