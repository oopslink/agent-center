import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/renderWith';
import { server } from '@/test/mswServer';
import { TopCostTasks } from './TopCostTasks';
import type { AnalyticsTopTask } from '@/api/types';

const tasks: AnalyticsTopTask[] = [
  {
    task_id: 'task-1',
    title: 'Scaffold control-flow upgrade',
    dominant_model: 'claude-opus-4-8',
    events: 5,
    tokens_in: 1000,
    tokens_out: 500,
    cache_tokens: 0,
    cost_micros: 42_180_000,
  },
  {
    // title unresolved → must fall back to the task_id, never a blank row.
    task_id: 'task-2',
    title: '',
    dominant_model: '',
    events: 2,
    tokens_in: 100,
    tokens_out: 50,
    cache_tokens: 0,
    cost_micros: 11_200_000,
  },
];

describe('TopCostTasks', () => {
  afterEach(() => cleanup());

  it('ranks tasks with title (fallback to id), cost, and model label', () => {
    renderWithQuery(<TopCostTasks tasks={tasks} agentId="a1" />);
    expect(screen.getByTestId('top-task-label-task-1')).toHaveTextContent('Scaffold control-flow upgrade');
    expect(screen.getByTestId('top-task-cost-task-1')).toHaveTextContent('$42.18');
    // unresolved title → task_id shown.
    expect(screen.getByTestId('top-task-label-task-2')).toHaveTextContent('task-2');
  });

  it('drills down into a task\'s usage events on click', async () => {
    server.use(
      http.get('/api/agents/:id/analytics/tasks/:taskId', () =>
        HttpResponse.json({
          task_id: 'task-1',
          events: [
            {
              id: 'ue1',
              project_id: 'p1',
              task_id: 'task-1',
              model: 'claude-opus-4-8',
              tokens_in: 100,
              tokens_out: 50,
              cache_read_tokens: 0,
              cache_write_tokens: 0,
              cost_micros: 900_000,
              ts: '2026-06-20T10:00:00Z',
              source: 'report',
            },
          ],
        }),
      ),
    );
    renderWithQuery(<TopCostTasks tasks={tasks} agentId="a1" />);
    fireEvent.click(screen.getByTestId('top-task-label-task-1'));
    await waitFor(() => expect(screen.getByTestId('top-task-drill-task-1')).toBeInTheDocument());
    const drill = screen.getByTestId('top-task-drill-task-1');
    await waitFor(() => expect(within(drill).getByText('$0.90')).toBeInTheDocument());
  });

  it('renders an empty state with no tasks', () => {
    renderWithQuery(<TopCostTasks tasks={[]} agentId="a1" />);
    expect(screen.getByTestId('analytics-top-tasks-empty')).toBeInTheDocument();
  });
});
