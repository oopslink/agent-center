import type React from 'react';
import { useSystemVersion } from '@/api/system';
import { formatLocalTime } from '@/utils/time';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { SYSTEM_SEGMENTS } from './systemSegments';

// Settings page. First panel (@oopslink): the server's build/version identity
// (version = ${branch}-${git-hash}). More settings land in a later ST.
export default function Settings(): React.ReactElement {
  const version = useSystemVersion();
  return (
    <section className="space-y-4" data-testid="page-Settings">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings) — desktop keeps the col② nav. */}
      <SegmentedNav items={SYSTEM_SEGMENTS} ariaLabel="System sections" />
      <h1 className="text-xl font-semibold">Settings</h1>

      <div
        className="max-w-md rounded-lg border border-border-base bg-bg-elevated p-4"
        data-testid="version-panel"
      >
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Version</h3>
        {version.isLoading && (
          <p className="text-xs text-text-muted" data-testid="version-loading">
            Loading version…
          </p>
        )}
        {version.isError && (
          <p className="text-xs text-danger" data-testid="version-error">
            Failed to load version info.
          </p>
        )}
        {version.data && (
          <dl className="space-y-2 text-sm">
            <VersionRow label="Version" value={version.data.version} mono testId="version-version" />
            <VersionRow label="Branch" value={version.data.branch} mono testId="version-branch" />
            <VersionRow label="Commit" value={version.data.commit} mono testId="version-commit" />
            <VersionRow
              label="Built"
              value={formatLocalTime(version.data.built_at)}
              title={version.data.built_at}
              testId="version-built-at"
            />
          </dl>
        )}
      </div>
    </section>
  );
}

function VersionRow({
  label,
  value,
  mono,
  title,
  testId,
}: {
  label: string;
  value: string;
  mono?: boolean;
  title?: string;
  testId: string;
}): React.ReactElement {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-xs uppercase tracking-wide text-text-muted">{label}</dt>
      {/* hash/version in tabular mono so it's selectable + copyable; raw
          built_at on hover via title. */}
      <dd
        className={`text-text-secondary ${mono ? 'select-all font-mono' : ''}`}
        data-testid={testId}
        title={title}
      >
        {value}
      </dd>
    </div>
  );
}
