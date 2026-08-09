import { describe, expect, it } from 'vitest';
import { tokenizeEntityRefs } from './entityRefCore';

describe('entityRefCore tokenizer', () => {
  it('finds short org refs and stable ids in the same text', () => {
    expect(tokenizeEntityRefs('P86 includes task-5ea6a6e8').map((t) => [t.kind, t.token])).toEqual([
      ['plan', 'P86'],
      ['task', 'task-5ea6a6e8'],
    ]);
  });

  it('does not match inside URLs, inline code, fenced code, or longer tokens', () => {
    const text = [
      'https://example.test/tasks/task-5ea6a6e8',
      '`task-5ea6a6e8`',
      '```',
      'P86 task-5ea6a6e8',
      '```',
      'subtask-5ea6a6e8',
      'foo_task-5ea6a6e8',
      'plain task-5ea6a6e8',
    ].join('\n');
    expect(tokenizeEntityRefs(text).map((t) => t.token)).toEqual(['task-5ea6a6e8']);
  });
});
