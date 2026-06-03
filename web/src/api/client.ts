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

// v2.6-FE-6: paths that must NOT receive auto-injected org_slug. /auth/* runs
// before org context exists; /orgs is the cross-org meta endpoint.
const ORG_INJECT_EXEMPT = ['/auth/', '/orgs', '/health'];

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

// withOrgSlug appends ?org_slug=<current> to a /api path. Exported so the few
// raw-fetch call sites (AddWorkerModal, Fleet worker actions, InstallCommandModal)
// that bypass the api client still carry the org scope. Pass an /api-relative
// path (e.g. "/workers/x/name"); the helper applies the same exemptions as the
// client's auto-injection.
export function withOrgSlug(path: string): string {
  if (!shouldInjectOrgSlug(path)) return path;
  const slug = readCurrentOrgSlug();
  if (!slug) return path;
  // Don't override if caller already set the param.
  if (path.includes('org_slug=') || path.includes('org_id=')) return path;
  return path + (path.includes('?') ? '&' : '?') + 'org_slug=' + encodeURIComponent(slug);
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
