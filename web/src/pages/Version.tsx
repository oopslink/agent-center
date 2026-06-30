import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useSystemVersion } from '@/api/system';
import { formatLocalTime } from '@/utils/time';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { useSystemSegments } from './useSystemSegments';

// Version page (/version). I7-D3: the build/version-identity panel is hoisted
// out of Settings into its own System-level page, sibling to Environment /
// Settings (System 第二级导航 = Environment / Settings / Version). The data is
// the server's build identity (version = ${branch}-${git-hash}) from
// /api/system/version — unchanged, only relocated.
export default function Version(): React.ReactElement {
  const { t } = useTranslation('common');
  const version = useSystemVersion();
  const systemSegments = useSystemSegments();
  return (
    <section className="space-y-4" data-testid="page-Version">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings | Version) — desktop keeps the rail's col② nav. */}
      <SegmentedNav items={systemSegments.segments} ariaLabel={systemSegments.ariaLabel} />
      <h1 className="text-xl font-semibold">{t('version.title')}</h1>

      <div
        className="max-w-md rounded-lg border border-border-base bg-bg-elevated p-4"
        data-testid="version-panel"
      >
        {version.isLoading && (
          <p className="text-xs text-text-muted" data-testid="version-loading">
            {t('version.loading')}
          </p>
        )}
        {version.isError && (
          <p className="text-xs text-danger" data-testid="version-error">
            {t('version.error')}
          </p>
        )}
        {version.data && (
          <dl className="space-y-2 text-sm">
            <VersionRow label={t('version.fields.version')} value={version.data.version} mono testId="version-version" />
            <VersionRow label={t('version.fields.branch')} value={version.data.branch} mono testId="version-branch" />
            <VersionRow label={t('version.fields.commit')} value={version.data.commit} mono testId="version-commit" />
            <VersionRow
              label={t('version.fields.built')}
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
