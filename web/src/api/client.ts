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

export async function request<T>(path: string, init: RequestInitWithTimeout = {}): Promise<T> {
  const { timeoutMs = DEFAULT_TIMEOUT_MS, headers, ...rest } = init;
  const fetchPromise = fetch(`/api${path}`, {
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
    if (err instanceof ApiError) throw err;
    if (err instanceof Error) {
      throw new ApiError(0, 'network_error', err.message);
    }
    throw new ApiError(0, 'unknown_error', String(err));
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
