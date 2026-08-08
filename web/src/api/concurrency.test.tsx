import { describe, expect, it } from 'vitest';
import { CONCURRENCY_POLL_MS } from './concurrency';

describe('agent concurrency hook contract', () => {
  it('polls live executor slots every 3 seconds', () => {
    expect(CONCURRENCY_POLL_MS).toBe(3000);
  });
});
