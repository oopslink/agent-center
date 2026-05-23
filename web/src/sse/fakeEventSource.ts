// FakeEventSource — minimal EventSource stand-in for tests. Captures
// the URL it was constructed with + exposes hooks to push events /
// trigger errors / inspect close calls. Mirrors only the surface that
// useSSE touches (onopen / onmessage / onerror / close + lastEventId).

export class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  closed = false;
  // EventSource interface fields used by useSSE.
  onopen: ((this: EventSource, ev: Event) => unknown) | null = null;
  onmessage: ((this: EventSource, ev: MessageEvent) => unknown) | null = null;
  onerror: ((this: EventSource, ev: Event) => unknown) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  static reset(): void {
    FakeEventSource.instances.length = 0;
  }

  static last(): FakeEventSource | undefined {
    return FakeEventSource.instances[FakeEventSource.instances.length - 1];
  }

  /** Simulate server `event:` line with parsed-JSON body + optional id. */
  emit(eventType: string, data: unknown, id?: string): void {
    if (this.closed) return;
    const ev = new MessageEvent('message', {
      data: JSON.stringify({ event_type: eventType, ...(data as object) }),
      lastEventId: id ?? '',
    });
    // The cast keeps TS happy — fake event handler signature is identical.
    this.onmessage?.call(this as unknown as EventSource, ev);
  }

  /** Simulate the server-confirmed connect. */
  openConnection(): void {
    if (this.closed) return;
    this.onopen?.call(this as unknown as EventSource, new Event('open'));
  }

  /** Simulate a transport failure. useSSE closes + reconnects. */
  fail(): void {
    if (this.closed) return;
    this.onerror?.call(this as unknown as EventSource, new Event('error'));
  }

  close(): void {
    this.closed = true;
  }
}
