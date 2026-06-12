// API client. Thin fetch wrapper — no axios per F4 oversight #1.
//
// Conventions:
//   - base URL is `/api`; dev server proxies to 127.0.0.1:7100,
//     production binary serves from the same origin.
//   - JSON in / JSON out.
//   - Non-2xx responses parse the `{error, message}` envelope and throw
//     ApiError so react-query mutations / queries surface a typed error.
//   - 10s timeout via Promise.race. (Native AbortController is not used
//     because jsdom installs a DOM-spec AbortSignal that fails undici's
//     `instanceof` check inside MSW v2 — Promise.race side-steps the
//     polyfill mismatch and is sufficient for a UI client.)

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(`[${status} ${code}] ${message}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

const DEFAULT_TIMEOUT_MS = 10_000;

type RequestInitWithTimeout = RequestInit & { timeoutMs?: number };

// v2.9 org-routing: resource paths that must NOT be scoped under /orgs/{slug}.
// These are matched against the /api-relative path (the leading "/api" prefix,
// if present, is stripped before the check). The locked exempt set is:
//   /auth/*   — runs before org context exists
//   /orgs     — org CRUD itself (/orgs, /orgs/{id}); cross-org meta endpoint
//   /users/   — cross-org user profile (/users/{user_id})
//   /sse      — per-user SSE stream + subscribe/unsubscribe
//   /health   — liveness probe
//   /system/  — system endpoints
// NB: /orgs only ever appears here as an INPUT path (org CRUD). The org-scoped
// form /orgs/{slug}/<resource> is the OUTPUT of withOrgSlug, never an input.
const ORG_INJECT_EXEMPT = ['/auth/', '/orgs', '/users/', '/sse', '/health', '/system/'];

function shouldInjectOrgSlug(path: string): boolean {
  for (const p of ORG_INJECT_EXEMPT) {
    if (path.startsWith(p)) return false;
  }
  return true;
}

// readCurrentOrgSlug extracts the org slug from /organizations/{slug}/... in
// the browser URL. Returns null when not on an org-scoped route or in non-
// browser environments (tests not using jsdom history).
function readCurrentOrgSlug(): string | null {
  try {
    if (typeof window === 'undefined' || !window.location) return null;
    const m = window.location.pathname.match(/^\/organizations\/([a-z0-9-]+)/);
    return m ? m[1] : null;
  } catch {
    return null;
  }
}

// withOrgSlug splices /orgs/{currentSlug} into a path so org-scoped requests
// hit the v2.9 path-routed backend (GET /api/orgs/{slug}/projects, …) instead
// of the legacy ?org_slug= query form. Exported so the few raw-fetch call sites
// (AddWorkerModal, Fleet worker actions, InstallCommandModal, MessageList,
// conversations upload, projects delete) that bypass the api client still carry
// the org scope.
//
// Two input conventions are accepted:
//   - /api-relative paths from request()/api.* (e.g. "/projects") → returns
//     "/orgs/{slug}/projects" (request() then prepends "/api").
//   - full paths from raw fetch() sites (e.g. "/api/workers/x") → returns
//     "/api/orgs/{slug}/workers/x" (the /orgs/{slug} segment is inserted AFTER
//     the leading /api, never before it).
//
// Returns the path unchanged when: the resource is exempt (see
// ORG_INJECT_EXEMPT), there is no org slug in the browser URL, or the path is
// already org-scoped (defensive guard against double-prefixing).
export function withOrgSlug(path: string): string {
  // Normalise: separate an optional leading "/api" prefix from the resource
  // path so exemption + splicing operate on the /api-relative resource.
  const hasApiPrefix = path === '/api' || path.startsWith('/api/');
  const prefix = hasApiPrefix ? '/api' : '';
  const resource = hasApiPrefix ? path.slice('/api'.length) : path;

  if (!shouldInjectOrgSlug(resource)) return path;
  // Already org-scoped — don't double-prefix.
  if (resource === '/orgs' || resource.startsWith('/orgs/')) return path;
  const slug = readCurrentOrgSlug();
  if (!slug) return path;
  return `${prefix}/orgs/${encodeURIComponent(slug)}${resource}`;
}

export async function request<T>(path: string, init: RequestInitWithTimeout = {}): Promise<T> {
  const { timeoutMs = DEFAULT_TIMEOUT_MS, headers, ...rest } = init;
  const finalPath = withOrgSlug(path);
  const fetchPromise = fetch(`/api${finalPath}`, {
    ...rest,
    headers: { 'Content-Type': 'application/json', ...(headers ?? {}) },
  }).then(async (resp) => {
    if (!resp.ok) {
      const errBody = await safeJSON<{ error?: string; message?: string }>(resp);
      throw new ApiError(
        resp.status,
        errBody?.error ?? 'http_error',
        errBody?.message ?? resp.statusText,
      );
    }
    if (resp.status === 204) {
      return undefined as T;
    }
    return (await resp.json()) as T;
  });

  try {
    return await Promise.race([
      fetchPromise,
      new Promise<T>((_, reject) =>
        setTimeout(
          () => reject(new ApiError(0, 'timeout', `request timed out after ${timeoutMs}ms`)),
          timeoutMs,
        ),
      ),
    ]);
  } catch (err) {
    if (err instanceof ApiError) {
      // On 401 (except /auth/* endpoints which handle auth themselves) route the
      // visitor to the right first screen — /signup on a fresh install, /signin
      // otherwise (v2.7 #145). The decision uses the public bootstrap probe so a
      // fresh install lands on register, not a login page it can't use yet.
      if (err.status === 401 && !path.startsWith('/auth/')) {
        void redirectUnauthenticated();
      }
      throw err;
    }
    if (err instanceof Error) {
      throw new ApiError(0, 'network_error', err.message);
    }
    throw new ApiError(0, 'unknown_error', String(err));
  }
}

// v2.7 #145: decide the unauthenticated first screen via the public bootstrap
// probe — /signup when the system has no users yet (fresh install), /signin
// otherwise. Guarded so concurrent 401s don't double-navigate.
let redirectingUnauthenticated = false;
async function redirectUnauthenticated(): Promise<void> {
  if (redirectingUnauthenticated) return;
  redirectingUnauthenticated = true;
  let target = '/signin';
  try {
    const res = await fetch('/api/auth/bootstrap', { credentials: 'same-origin' });
    if (res.ok) {
      const body = (await res.json()) as { initialized?: boolean };
      if (body.initialized === false) target = '/signup';
    }
  } catch {
    // Network error → fall back to /signin.
  }
  if (window.location.pathname !== target) {
    window.location.href = target;
  } else {
    redirectingUnauthenticated = false;
  }
}

async function safeJSON<T>(resp: Response): Promise<T | null> {
  try {
    return (await resp.json()) as T;
  } catch {
    return null;
  }
}

export const api = {
  get: <T>(path: string) => request<T>(path, { method: 'GET' }),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'POST',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PATCH',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  del: <T = void>(path: string) => request<T>(path, { method: 'DELETE' }),
};
