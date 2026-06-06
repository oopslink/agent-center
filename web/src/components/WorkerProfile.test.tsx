import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { WorkerProfile } from './WorkerProfile';
import type { EnvWorker } from '@/api/types';

const w = (extra: Partial<EnvWorker> = {}): EnvWorker => ({
  worker_id: 'worker-abc',
  organization_id: 'O',
  name: 'My Worker',
  status: 'online',
  last_acked_offset: 0,
  enrolled_at: '2026-06-06T10:00:00Z',
  last_heartbeat_at: '2026-06-06T12:00:00Z',
  created_at: '2026-06-06T09:00:00Z',
  updated_at: '2026-06-06T12:00:00Z',
  version: 1,
  ...extra,
});

describe('WorkerProfile', () => {
  it('renders the 5 real fields (registration = enrolled_at, not created_at)', () => {
    render(<WorkerProfile worker={w()} />);
    expect(screen.getByTestId('worker-profile-id')).toHaveTextContent('worker-abc');
    expect(screen.getByTestId('worker-profile-name')).toHaveTextContent('My Worker');
    expect(screen.getByTestId('worker-profile-status')).toHaveTextContent('Online');
    expect(screen.getByTestId('worker-profile-enrolled')).toHaveAttribute(
      'title',
      '2026-06-06T10:00:00Z',
    );
    expect(screen.getByTestId('worker-profile-heartbeat')).toBeInTheDocument();
  });

  it('shows the 5 deferred fields as "Coming in v2.9" (explicit, not blank/fake)', () => {
    render(<WorkerProfile worker={w()} />);
    for (const s of ['hostname', 'os', 'architecture', 'agent-center-version', 'install-path']) {
      expect(screen.getByTestId(`worker-profile-deferred-${s}`)).toHaveTextContent('Coming in v2.9');
    }
  });

  it('offline status badge', () => {
    render(<WorkerProfile worker={w({ status: 'offline' })} />);
    expect(screen.getByTestId('worker-profile-status')).toHaveTextContent('Offline');
  });

  it('missing enrolled_at → em dash (no crash)', () => {
    render(<WorkerProfile worker={w({ enrolled_at: undefined })} />);
    expect(screen.getByTestId('worker-profile-enrolled')).toHaveTextContent('—');
  });

  it('unnamed worker → "unnamed"', () => {
    render(<WorkerProfile worker={w({ name: '' })} />);
    expect(screen.getByTestId('worker-profile-name')).toHaveTextContent('unnamed');
  });
});
