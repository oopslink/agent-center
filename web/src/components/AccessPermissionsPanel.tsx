import type React from 'react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { AccessMetaPill, accessResourceLabel } from '@/components/access/kit';
import {
  useEffectivePermissions,
  useGrantDirectPermission,
  usePermissionAudit,
  usePermissionDefinitions,
  usePermissionExplain,
  useRevokeDirectPermission,
  type EffectivePermission,
  type PermissionDefinition,
  type PermissionAuditEvent,
  type ResourceScope,
} from '@/api/permissions';

interface Props {
  subjectRef: string;
  subjectLabel: string;
  resource: ResourceScope | null;
  resourceLabel: string;
}

type Notice = { tone: 'success' | 'warning' | 'danger'; text: string } | null;

const DIRECT_SOURCE = 'custom_role';

function isDirect(p: EffectivePermission): boolean {
  return p.source === DIRECT_SOURCE;
}

function humanize(v: string): string {
  return v.replace(/[_:.]+/g, ' ');
}

function bareSubjectID(subjectRef: string): string {
  const i = subjectRef.indexOf(':');
  return i >= 0 ? subjectRef.slice(i + 1) : subjectRef;
}

function formatTime(iso: string): string {
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function sourceLabel(source: string, t: (key: string, options?: Record<string, unknown>) => string): string {
  return t(`access.sources.${source}`, { defaultValue: humanize(source) });
}

function resourceText(resource: ResourceScope): string {
  return accessResourceLabel(resource);
}

function resourceKindLabel(kind: string): string {
  if (kind === 'org') return 'Organization';
  if (kind === 'admin_token') return 'Admin token';
  return kind.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
}

function actionLabel(action: string): string {
  const normalized = action === 'update' ? 'write' : action;
  return normalized.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
}

function semanticPermissionLabel(key: string, definitions: PermissionDefinition[] | undefined, resource: ResourceScope | null): string {
  const definition = definitions?.find((entry) => entry.key === key);
  const kind = resource?.kind || definition?.resource_kinds[0] || key.split('.')[0] || 'resource';
  const resourceLabel = resourceKindLabel(kind);
  const scopeLabel = kind === 'org' ? 'Org-wide' : `This ${resourceLabel.toLowerCase()}`;
  const action = actionLabel(definition?.actions[0] || key.split('.').at(-1) || 'use');
  return `${resourceLabel} · ${scopeLabel} · ${action}`;
}

function evidenceText(p: EffectivePermission): string {
  return p.evidence_ref || p.assignment_id || p.role_id || 'source';
}

function sourceHref(p: EffectivePermission, resource: ResourceScope, subjectRef: string): string | null {
  if (p.source === 'org_role') {
    const id = bareSubjectID(subjectRef);
    return subjectRef.startsWith('agent:') ? `/agents/${encodeURIComponent(id)}` : `/users/${encodeURIComponent(id)}`;
  }
  if (p.source === 'project_member') {
    if (resource.kind === 'project' && resource.id) return `/projects/${encodeURIComponent(resource.id)}?tab=members`;
    if (resource.kind === 'task' && resource.project_id && resource.id) {
      return `/projects/${encodeURIComponent(resource.project_id)}/tasks/${encodeURIComponent(resource.id)}`;
    }
    if (resource.kind === 'issue' && resource.project_id && resource.id) {
      return `/projects/${encodeURIComponent(resource.project_id)}/issues/${encodeURIComponent(resource.id)}`;
    }
    if (resource.kind === 'plan' && resource.project_id && resource.id) {
      return `/projects/${encodeURIComponent(resource.project_id)}/plans/${encodeURIComponent(resource.id)}`;
    }
  }
  if (p.source === 'team_member' || p.source === 'team_memory_policy') {
    const raw = p.evidence_ref.split(':')[1] ?? '';
    const teamID = raw.split('/')[0];
    return teamID ? `/teams/${encodeURIComponent(teamID)}` : null;
  }
  if (p.source === 'agent_worker_binding' && resource.id) {
    return `/agents/${encodeURIComponent(resource.id)}`;
  }
  if (p.source === 'worker_owner' && resource.kind === 'worker' && resource.id) {
    return `/workers/${encodeURIComponent(resource.id)}`;
  }
  return null;
}

function eventSentence(e: PermissionAuditEvent, t: (key: string, options?: Record<string, unknown>) => string): string {
  const permission = e.permission_key || String(e.payload?.permission_key ?? '');
  const resource = e.resource_kind && e.resource_id ? `${e.resource_kind}:${e.resource_id}` : '';
  switch (e.event_type) {
    case 'authorization.assignment.created':
      return t('access.audit.assignmentCreated', { permission, resource });
    case 'authorization.assignment.revoked':
      return t('access.audit.assignmentRevoked', { permission, resource });
    case 'authorization.role.upserted':
      return t('access.audit.roleUpserted', { role: e.role_id || String(e.payload?.name ?? '') });
    case 'authorization.role_permissions.set':
      return t('access.audit.rolePermissionsSet', { role: e.role_id });
    default:
      return e.event_type;
  }
}

function EmptyLine({ children, testId }: { children: React.ReactNode; testId: string }): React.ReactElement {
  return (
    <p className="py-2 text-xs text-text-muted" data-testid={testId}>
      {children}
    </p>
  );
}

function PermissionPill({ permission, label }: { permission: string; label?: string }): React.ReactElement {
  return (
    <span className="inline-flex min-w-0 flex-col rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] text-text-primary">
      <span className="truncate font-semibold">{label ?? permission}</span>
      {label && <span className="truncate font-mono text-text-muted">{permission}</span>}
    </span>
  );
}

function SourceCell({
  permission,
  resource,
  subjectRef,
}: {
  permission: EffectivePermission;
  resource: ResourceScope;
  subjectRef: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const href = sourceHref(permission, resource, subjectRef);
  const label = sourceLabel(permission.source, t);
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
      <span className="font-medium text-text-primary">{label}</span>
      <span className="min-w-0 truncate font-mono text-[0.6875rem] text-text-muted" title={evidenceText(permission)}>
        {evidenceText(permission)}
      </span>
      {href && (
        <OrgLink
          to={href}
          className="shrink-0 rounded text-xs text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          data-testid="access-source-link"
        >
          {t('access.source.open')}
        </OrgLink>
      )}
    </span>
  );
}

function PermissionRows({
  rows,
  resource,
  subjectRef,
  empty,
  testId,
  definitions,
}: {
  rows: EffectivePermission[];
  resource: ResourceScope;
  subjectRef: string;
  empty: string;
  testId: string;
  definitions?: PermissionDefinition[];
}): React.ReactElement {
  if (rows.length === 0) {
    return <EmptyLine testId={`${testId}-empty`}>{empty}</EmptyLine>;
  }
  return (
    <ul className="divide-y divide-border-base" data-testid={testId}>
      {rows.map((p) => (
        <li
          key={`${p.key}:${p.source}:${p.evidence_ref}:${p.assignment_id ?? ''}`}
          className="grid gap-2 py-2 text-sm md:grid-cols-[minmax(12rem,1fr)_minmax(16rem,1.4fr)_5rem]"
          data-testid="access-permission-row"
          data-permission-key={p.key}
          data-source={p.source}
        >
          <span className="min-w-0">
            <PermissionPill permission={p.key} label={semanticPermissionLabel(p.key, definitions, resource)} />
          </span>
          <SourceCell permission={p} resource={resource} subjectRef={subjectRef} />
          <AccessMetaPill>{p.delegatable ? 'delegable' : 'fixed'}</AccessMetaPill>
        </li>
      ))}
    </ul>
  );
}

export function AccessPermissionsPanel({
  subjectRef,
  subjectLabel,
  resource,
  resourceLabel,
}: Props): React.ReactElement {
  const { t } = useTranslation('members');
  const definitions = usePermissionDefinitions();
  const effective = useEffectivePermissions(subjectRef, resource);
  const audit = usePermissionAudit(subjectRef, !!resource);
  const grant = useGrantDirectPermission();
  const revoke = useRevokeDirectPermission();
  const [selectedPermission, setSelectedPermission] = useState('');
  const [notice, setNotice] = useState<Notice>(null);

  const permissions = effective.data?.permissions ?? [];
  const direct = permissions.filter(isDirect);
  const inherited = permissions.filter((p) => !isDirect(p));
  const candidatePermissions = useMemo(() => {
    const keys = new Set<string>();
    for (const d of definitions.data?.definitions ?? []) {
      if (!resource || d.resource_kinds.includes(resource.kind)) keys.add(d.key);
    }
    for (const p of permissions) keys.add(p.key);
    return [...keys].sort();
  }, [definitions.data?.definitions, permissions, resource]);
  const selected = candidatePermissions.includes(selectedPermission)
    ? selectedPermission
    : candidatePermissions[0] ?? '';
  const explain = usePermissionExplain(subjectRef, selected, resource, !!selected);
  const selectedAlreadyDirect = direct.some((p) => p.key === selected);

  const onGrant = async () => {
    if (!resource || !selected) return;
    setNotice(null);
    try {
      await grant.mutateAsync({ subjectRef, permissionKey: selected, resource });
      await effective.refetch();
      await audit.refetch();
      setNotice({ tone: 'success', text: t('access.notice.granted', { permission: selected }) });
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : String(err) });
    }
  };

  const onRevoke = async (p: EffectivePermission) => {
    if (!p.assignment_id) return;
    setNotice(null);
    try {
      await revoke.mutateAsync({ assignmentId: p.assignment_id });
      const latest = await effective.refetch();
      await audit.refetch();
      const inheritedMatch = (latest.data?.permissions ?? []).find((x) => x.key === p.key && !isDirect(x));
      if (inheritedMatch) {
        setNotice({
          tone: 'warning',
          text: t('access.notice.revokedInherited', {
            permission: p.key,
            source: sourceLabel(inheritedMatch.source, t),
            evidence: evidenceText(inheritedMatch),
          }),
        });
      } else {
        setNotice({ tone: 'success', text: t('access.notice.revoked', { permission: p.key }) });
      }
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : String(err) });
    }
  };

  if (!resource) {
    return (
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-permissions-panel">
        <EmptyLine testId="access-unavailable">{t('access.unavailable')}</EmptyLine>
      </section>
    );
  }

  const explainRows = explain.data?.effective ?? [];
  const auditEvents = audit.data?.events ?? [];
  const noticeClass =
    notice?.tone === 'danger'
      ? 'border-danger/30 bg-danger/10 text-danger'
      : notice?.tone === 'warning'
        ? 'border-warning/30 bg-warning/10 text-warning'
        : 'border-success/30 bg-success/10 text-success';

  return (
    <section className="space-y-4" data-testid="access-permissions-panel" data-subject-ref={subjectRef}>
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-overview">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">{t('access.overview.title')}</h3>
            <p className="mt-1 text-xs text-text-muted">
              {subjectLabel} · <span className="font-mono">{subjectRef}</span>
            </p>
          </div>
          <span className="rounded bg-bg-subtle px-2 py-1 font-mono text-xs text-text-secondary">
            {resourceLabel || resourceText(resource)}
          </span>
        </div>
        <dl className="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-xs text-text-muted">{t('access.overview.total')}</dt>
            <dd className="text-lg font-semibold text-text-primary" data-testid="access-overview-total">
              {permissions.length}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-text-muted">{t('access.overview.direct')}</dt>
            <dd className="text-lg font-semibold text-text-primary" data-testid="access-overview-direct">
              {direct.length}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-text-muted">{t('access.overview.inherited')}</dt>
            <dd className="text-lg font-semibold text-text-primary" data-testid="access-overview-inherited">
              {inherited.length}
            </dd>
          </div>
        </dl>
        {effective.isLoading && <EmptyLine testId="access-loading">{t('access.loading')}</EmptyLine>}
        {effective.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="access-error">
            {(effective.error as Error).message}
          </p>
        )}
      </section>

      {notice && (
        <p className={`rounded border px-3 py-2 text-sm ${noticeClass}`} data-testid="access-revoke-notice">
          {notice.text}
        </p>
      )}

      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.effective.title')}</h3>
        <PermissionRows
          rows={permissions}
          resource={effective.data?.resource ?? resource}
          subjectRef={subjectRef}
          empty={t('access.effective.empty')}
          testId="access-effective-list"
          definitions={definitions.data?.definitions}
        />
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-explain-tree">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h3 className="text-sm font-semibold text-text-primary">{t('access.explain.title')}</h3>
          <select
            value={selected}
            onChange={(e) => setSelectedPermission(e.target.value)}
            className="min-h-[36px] rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary"
            data-testid="access-explain-select"
          >
            {candidatePermissions.map((key) => (
              <option key={key} value={key}>
                {semanticPermissionLabel(key, definitions.data?.definitions, resource)}
              </option>
            ))}
          </select>
        </div>
        {definitions.isError && (
          <p className="mt-2 text-xs text-warning" data-testid="access-definitions-error">
            {t('access.definitionsError')}
          </p>
        )}
        {explain.isLoading && <EmptyLine testId="access-explain-loading">{t('access.loading')}</EmptyLine>}
        {explain.data && (
          <div className="mt-3 space-y-2 text-sm">
            <div className="grid gap-2 md:grid-cols-[8rem_1fr]">
              <span className="text-text-muted">{t('access.explain.subject')}</span>
              <span className="font-mono text-text-primary">{explain.data.decision.subject_ref}</span>
              <span className="text-text-muted">{t('access.explain.resource')}</span>
              <span className="font-mono text-text-primary">{resourceText(explain.data.decision.resource)}</span>
              <span className="text-text-muted">{t('access.explain.decision')}</span>
              <span
                className={explain.data.decision.allowed ? 'text-success' : 'text-danger'}
                data-testid="access-explain-decision"
              >
                {explain.data.decision.allowed ? t('access.explain.allowed') : t('access.explain.denied')}
                {' · '}
                {explain.data.decision.reason}
              </span>
            </div>
            {explainRows.length > 0 ? (
              <ul className="divide-y divide-border-base" data-testid="access-explain-sources">
                {explainRows.map((p) => (
                  <li key={`${p.key}:${p.source}:${p.evidence_ref}`} className="flex flex-wrap items-center gap-2 py-1.5">
                    <PermissionPill permission={p.key} label={semanticPermissionLabel(p.key, definitions.data?.definitions, explain.data.decision.resource)} />
                    <SourceCell permission={p} resource={explain.data.decision.resource} subjectRef={subjectRef} />
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyLine testId="access-explain-empty">{t('access.explain.empty')}</EmptyLine>
            )}
            {(explain.data.denied_by ?? []).length > 0 && (
              <ul className="space-y-1 text-xs text-text-muted" data-testid="access-explain-denied-by">
                {explain.data.denied_by?.map((line) => <li key={line}>{line}</li>)}
              </ul>
            )}
          </div>
        )}
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-direct-grants">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.direct.title')}</h3>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <select
            value={selected}
            onChange={(e) => setSelectedPermission(e.target.value)}
            className="min-h-[36px] min-w-[16rem] rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary"
            data-testid="access-grant-permission"
          >
            {candidatePermissions.map((key) => (
              <option key={key} value={key}>
                {semanticPermissionLabel(key, definitions.data?.definitions, resource)}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="min-h-[36px] rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            onClick={() => void onGrant()}
            disabled={!selected || selectedAlreadyDirect || grant.isPending}
            data-testid="access-grant-submit"
          >
            {selectedAlreadyDirect ? t('access.direct.alreadyGranted') : grant.isPending ? t('access.direct.granting') : t('access.direct.grant')}
          </button>
        </div>
        {direct.length === 0 ? (
          <EmptyLine testId="access-direct-empty">{t('access.direct.empty')}</EmptyLine>
        ) : (
          <ul className="mt-3 divide-y divide-border-base" data-testid="access-direct-list">
            {direct.map((p) => (
              <li
                key={`${p.key}:${p.assignment_id ?? p.evidence_ref}`}
                className="flex flex-wrap items-center justify-between gap-3 py-2 text-sm"
                data-testid="access-direct-row"
              >
                <span className="flex min-w-0 flex-col gap-1">
                  <PermissionPill permission={p.key} label={semanticPermissionLabel(p.key, definitions.data?.definitions, resource)} />
                  <span className="truncate font-mono text-[0.6875rem] text-text-muted" title={evidenceText(p)}>
                    {evidenceText(p)}
                  </span>
                </span>
                <button
                  type="button"
                  className="min-h-[36px] rounded border border-danger/40 px-3 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
                  onClick={() => void onRevoke(p)}
                  disabled={!p.assignment_id || revoke.isPending}
                  aria-label={t('access.direct.revokeAria', { permission: p.key })}
                  data-testid="access-direct-revoke"
                >
                  {revoke.isPending ? t('access.direct.revoking') : t('access.direct.revoke')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.inherited.title')}</h3>
        <PermissionRows
          rows={inherited}
          resource={effective.data?.resource ?? resource}
          subjectRef={subjectRef}
          empty={t('access.inherited.empty')}
          testId="access-inherited-list"
          definitions={definitions.data?.definitions}
        />
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-audit">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.audit.title')}</h3>
        {audit.isLoading && <EmptyLine testId="access-audit-loading">{t('access.loading')}</EmptyLine>}
        {audit.isError && (
          <p className="mt-2 text-xs text-danger" data-testid="access-audit-error">
            {(audit.error as Error).message}
          </p>
        )}
        {!audit.isLoading && auditEvents.length === 0 && (
          <EmptyLine testId="access-audit-empty">{t('access.audit.empty')}</EmptyLine>
        )}
        {auditEvents.length > 0 && (
          <ul className="mt-2 divide-y divide-border-base" data-testid="access-audit-list">
            {auditEvents.map((e) => (
              <li key={e.id} className="grid gap-2 py-2 text-sm md:grid-cols-[8rem_1fr]" data-testid="access-audit-row">
                <time className="text-xs text-text-muted" dateTime={e.created_at} title={e.created_at}>
                  {formatTime(e.created_at)}
                </time>
                <span className="min-w-0 text-text-secondary">
                  <span className="font-medium text-text-primary">{humanize(e.actor_ref)}</span>
                  {' · '}
                  {eventSentence(e, t)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </section>
  );
}
