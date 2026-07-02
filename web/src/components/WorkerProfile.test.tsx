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

  it('shows the system-info fields as "Coming in v2.9" when the worker has not reported them', () => {
    render(<WorkerProfile worker={w()} />);
    for (const s of ['hostname', 'os', 'architecture', 'install-path', 'worker-version']) {
      expect(screen.getByTestId(`worker-profile-deferred-${s}`)).toHaveTextContent('Coming in v2.9');
    }
  });

  it('renders real system-info values when the worker reported them (T752)', () => {
    render(
      <WorkerProfile
        worker={w({
          hostname: 'dev001.local',
          os: 'darwin',
          arch: 'arm64',
          install_path: '/usr/local/bin/agent-center',
          worker_version: 'v2.10.2+abc1234',
        })}
      />,
    );
    expect(screen.getByTestId('worker-profile-hostname')).toHaveTextContent('dev001.local');
    expect(screen.getByTestId('worker-profile-os')).toHaveTextContent('darwin');
    expect(screen.getByTestId('worker-profile-architecture')).toHaveTextContent('arm64');
    expect(screen.getByTestId('worker-profile-install-path')).toHaveTextContent(
      '/usr/local/bin/agent-center',
    );
    expect(screen.getByTestId('worker-profile-worker-version')).toHaveTextContent('v2.10.2+abc1234');
    // no placeholder cells remain for the reported fields
    expect(screen.queryByTestId('worker-profile-deferred-hostname')).toBeNull();
    expect(screen.queryByTestId('worker-profile-deferred-worker-version')).toBeNull();
  });

  it('mixes real + placeholder per field (partial report is honest)', () => {
    render(<WorkerProfile worker={w({ hostname: 'h1', os: 'linux' })} />);
    expect(screen.getByTestId('worker-profile-hostname')).toHaveTextContent('h1');
    expect(screen.getByTestId('worker-profile-os')).toHaveTextContent('linux');
    // unreported ones still show the placeholder
    expect(screen.getByTestId('worker-profile-deferred-worker-version')).toHaveTextContent(
      'Coming in v2.9',
    );
    expect(screen.getByTestId('worker-profile-deferred-install-path')).toHaveTextContent(
      'Coming in v2.9',
    );
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
