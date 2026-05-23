import { describe, expect, it } from 'vitest';

// Mock-shape audit: the secret-related MSW responses are themselves
// part of the no-plaintext contract. A test mock that returns a `value`
// field would be the bad example — it would let frontend code that
// rendered plaintext slip past unit tests. So we hit the mock (the
// global setup in src/test/setup.ts already installed it) and assert
// NO `value` key in any secret-shaped response.
//
// This is a meta-test on our own fixtures, not a frontend behaviour
// test.

describe('MSW secret handlers (mock-shape audit)', () => {
  it('GET /api/secrets — every row excludes value + plaintext', async () => {
    const resp = await fetch('/api/secrets');
    const body = (await resp.json()) as Array<Record<string, unknown>>;
    expect(Array.isArray(body)).toBe(true);
    expect(body.length).toBeGreaterThan(0);
    for (const row of body) {
      expect(row).not.toHaveProperty('value');
      expect(row).not.toHaveProperty('plaintext');
      // Sanity: shape carries the public projection fields.
      expect(row).toHaveProperty('id');
      expect(row).toHaveProperty('name');
      expect(row).toHaveProperty('kind');
      expect(row).toHaveProperty('state');
    }
  });

  it('POST /api/secrets — create response is narrow: {id, name, event_id} only', async () => {
    const resp = await fetch('/api/secrets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'x', kind: 'other', value: 'leaked-if-echoed' }),
    });
    const body = (await resp.json()) as Record<string, unknown>;
    expect(body).not.toHaveProperty('value');
    expect(body).not.toHaveProperty('plaintext');
    // Match the actual backend create response shape — anything richer
    // is shape drift that risks leaking plaintext in future refactors.
    expect(Object.keys(body).sort()).toEqual(['event_id', 'id', 'name']);
  });

  it('DELETE /api/secrets/:id — revoke response is {revoked: true}, no leakage fields', async () => {
    const resp = await fetch('/api/secrets/S-1', { method: 'DELETE' });
    const body = (await resp.json()) as Record<string, unknown>;
    expect(body).toEqual({ revoked: true });
    expect(body).not.toHaveProperty('value');
  });
});
