// Minimal SSE subscriber. We don't pull in the npm `eventsource`
// package because (a) it's an extra dep and (b) doing the parse
// ourselves keeps full control of timeout / cleanup, which matters
// for Playwright anti-flake.
//
// Usage:
//
//   const stop = await subscribeSSE(`${baseURL}/api/sse?...`, (ev) => {
//     events.push(ev);
//   });
//   ...
//   stop();
//
// SSE wire format (RFC 8895-ish, MDN-ish): records are \n\n-
// separated; within a record, lines starting with `event:`, `data:`,
// `id:`, `retry:` carry the named fields. Lines starting with `:` are
// comments / heartbeats. Multi-line `data:` is concatenated with \n.

export interface SSEEvent {
  event?: string;
  data: string;
  id?: string;
}

export type SSEListener = (ev: SSEEvent) => void;

export async function subscribeSSE(
  url: string,
  listener: SSEListener,
  init: RequestInit = {},
): Promise<() => void> {
  const controller = new AbortController();
  const res = await fetch(url, {
    ...init,
    headers: { Accept: "text/event-stream", ...(init.headers ?? {}) },
    signal: controller.signal,
  });
  if (!res.ok || !res.body) {
    throw new Error(`SSE subscribe ${url}: HTTP ${res.status}`);
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";

  // Pump in background; the caller stops via the returned function.
  (async () => {
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) return;
        buffer += decoder.decode(value, { stream: true });
        let sep: number;
        while ((sep = buffer.indexOf("\n\n")) !== -1) {
          const raw = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          const ev = parseRecord(raw);
          if (ev !== null) listener(ev);
        }
      }
    } catch (e) {
      // Aborted fetches throw — that's the normal stop path; swallow.
      if ((e as Error).name !== "AbortError") {
        // Re-emit synchronously so the test can fail visibly.
        throw e;
      }
    }
  })();

  return () => controller.abort();
}

function parseRecord(raw: string): SSEEvent | null {
  let event: string | undefined;
  let id: string | undefined;
  const dataParts: string[] = [];
  for (const line of raw.split("\n")) {
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    switch (field) {
      case "event":
        event = value;
        break;
      case "data":
        dataParts.push(value);
        break;
      case "id":
        id = value;
        break;
    }
  }
  if (dataParts.length === 0 && event === undefined) return null;
  return { event, data: dataParts.join("\n"), id };
}
