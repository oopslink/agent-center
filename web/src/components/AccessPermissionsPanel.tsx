import type React from 'react';
import { useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import {
  useEffectivePermissions,
  usePermissionAudit,
  usePermissionDefinitions,
  usePermissionExplain,
  type EffectivePermission,
  type PermissionAuditEvent,
  type PermissionDefinition,
  type ResourceScope,
} from '@/api/permissions';

interface Props {
  subjectRef: string;
  subjectLabel: string;
  resource: ResourceScope | null;
  resourceLabel: string;
}

interface SourceDetail {
  label: string;
  fact: string;
  editLocation: string;
  href: string | null;
  risk: string | null;
}

interface AccessGraphEdge {
  id: string;
  source: string;
  sourceLabel: string;
  permissions: EffectivePermission[];
  risk: string | null;
}

interface CompletenessLike {
  complete?: boolean;
  truncated?: boolean;
  has_more?: boolean;
  warnings?: string[];
}

const CUSTOM_ROLE_SOURCE = 'custom_role';

function humanize(v: string): string {
  return v.replace(/[_:.]+/g, ' ');
}

function bareSubjectID(subjectRef: string): string {
  const i = subjectRef.indexOf(':');
  return i >= 0 ? subjectRef.slice(i + 1) : subjectRef;
}

function subjectKind(subjectRef: string, t: TFunction<'members'>): string {
  if (subjectRef.startsWith('user:')) return t('access.subject.kind.user');
  if (subjectRef.startsWith('agent:')) return t('access.subject.kind.agent');
  if (subjectRef.startsWith('worker:')) return t('access.subject.kind.worker');
  if (subjectRef === 'system') return t('access.subject.kind.system');
  return t('access.subject.kind.unknown');
}

function formatTime(iso: string): string {
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function sourceLabel(source: string, t: TFunction<'members'>): string {
  return t(`access.sources.${source}`, { defaultValue: humanize(source) });
}

function resourceText(resource: ResourceScope): string {
  const id = resource.uri || resource.id || resource.org_id || '*';
  return `${resource.kind}:${id}`;
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
    const projectID = resource.kind === 'project' ? resource.id : resource.project_id;
    return projectID ? `/projects/${encodeURIComponent(projectID)}?tab=members` : null;
  }
  if (p.source === 'team_member' || p.source === 'team_memory_policy') {
    const teamID = teamIDFromEvidence(p.evidence_ref) || (resource.kind === 'team' ? resource.id : '');
    return teamID ? `/teams/${encodeURIComponent(teamID)}?tab=mm` : null;
  }
  if (p.source === 'conversation_participant' && resource.kind === 'conversation' && resource.id) {
    return `/conversations/${encodeURIComponent(resource.id)}`;
  }
  if (p.source === 'agent_worker_binding' && resource.id) {
    return `/agents/${encodeURIComponent(resource.id)}`;
  }
  if (p.source === 'worker_owner' && resource.kind === 'worker' && resource.id) {
    return `/workers/${encodeURIComponent(resource.id)}`;
  }
  return null;
}

function teamIDFromEvidence(evidenceRef: string): string | null {
  if (!evidenceRef.startsWith('team_members:')) return null;
  const raw = evidenceRef.slice('team_members:'.length);
  return raw.split('/')[0] || null;
}

function teamRoleFromEvidence(evidenceRef: string): string | null {
  if (!evidenceRef.startsWith('team_members:')) return null;
  const parts = evidenceRef.slice('team_members:'.length).split('/');
  return parts.length >= 3 ? parts[parts.length - 1] || null : null;
}

function accessRoleLabel(permission: EffectivePermission, t: TFunction<'members'>): string {
  if (permission.source === CUSTOM_ROLE_SOURCE) {
    return permission.role_id || t('access.roles.customAccessRole');
  }
  if (permission.source === 'org_role') return t('access.roles.orgAccessRole');
  if (permission.source === 'project_member') return t('access.roles.projectAccessRole');
  if (permission.source === 'team_memory_policy') return t('access.roles.teamMemoryPolicy');
  return sourceLabel(permission.source, t);
}

function sourceDetail(
  permission: EffectivePermission,
  resource: ResourceScope,
  subjectRef: string,
  t: TFunction<'members'>,
): SourceDetail {
  const source = permission.source;
  const label = sourceLabel(source, t);
  const href = sourceHref(permission, resource, subjectRef);
  const fallbackRisk = source === CUSTOM_ROLE_SOURCE ? null : t('access.risks.legacyDerived');
  const bySource: Record<string, Omit<SourceDetail, 'label' | 'href'>> = {
    org_role: {
      fact: t('access.sourceFacts.org_role'),
      editLocation: t('access.editLocations.org_role'),
      risk: null,
    },
    project_member: {
      fact: t('access.sourceFacts.project_member'),
      editLocation: t('access.editLocations.project_member'),
      risk: null,
    },
    team_member: {
      fact: t('access.sourceFacts.team_member'),
      editLocation: t('access.editLocations.team_member'),
      risk: t('access.risks.teamRoleRuntime'),
    },
    team_memory_policy: {
      fact: t('access.sourceFacts.team_memory_policy'),
      editLocation: t('access.editLocations.team_memory_policy'),
      risk: t('access.risks.policyDerived'),
    },
    conversation_participant: {
      fact: t('access.sourceFacts.conversation_participant'),
      editLocation: t('access.editLocations.conversation_participant'),
      risk: null,
    },
    file_scope: {
      fact: t('access.sourceFacts.file_scope'),
      editLocation: t('access.editLocations.file_scope'),
      risk: t('access.risks.fileReachability'),
    },
    admin_token_scope: {
      fact: t('access.sourceFacts.admin_token_scope'),
      editLocation: t('access.editLocations.admin_token_scope'),
      risk: t('access.risks.internalOnly'),
    },
    worker_owner: {
      fact: t('access.sourceFacts.worker_owner'),
      editLocation: t('access.editLocations.worker_owner'),
      risk: t('access.risks.runtimeBinding'),
    },
    agent_worker_binding: {
      fact: t('access.sourceFacts.agent_worker_binding'),
      editLocation: t('access.editLocations.agent_worker_binding'),
      risk: t('access.risks.runtimeBinding'),
    },
    system: {
      fact: t('access.sourceFacts.system'),
      editLocation: t('access.editLocations.system'),
      risk: t('access.risks.internalOnly'),
    },
    custom_role: {
      fact: t('access.sourceFacts.custom_role'),
      editLocation: t('access.editLocations.custom_role'),
      risk: null,
    },
  };
  return { label, href, ...(bySource[source] ?? {
    fact: t('access.sourceFacts.unknown'),
    editLocation: t('access.editLocations.unknown'),
    risk: fallbackRisk,
  }) };
}

function eventSentence(e: PermissionAuditEvent, t: TFunction<'members'>): string {
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

function completenessState(data: CompletenessLike | undefined): 'complete' | 'truncated' | 'unknown' {
  if (!data) return 'unknown';
  if (data.truncated || data.has_more) return 'truncated';
  if (data.complete === true) return 'complete';
  return 'unknown';
}

function definitionKeysForResource(
  definitions: PermissionDefinition[] | undefined,
  resource: ResourceScope | null,
): string[] {
  const keys = new Set<string>();
  for (const d of definitions ?? []) {
    if (!resource || d.resource_kinds.includes(resource.kind)) keys.add(d.key);
  }
  return [...keys].sort();
}

function graphEdges(permissions: EffectivePermission[], t: TFunction<'members'>): AccessGraphEdge[] {
  const groups = new Map<string, EffectivePermission[]>();
  for (const p of permissions) {
    const key = `${p.source}:${p.evidence_ref || p.role_id || p.assignment_id || ''}`;
    const rows = groups.get(key) ?? [];
    rows.push(p);
    groups.set(key, rows);
  }
  return [...groups.entries()].map(([id, rows]) => {
    const first = rows[0];
    return {
      id,
      source: first?.source ?? 'unknown',
      sourceLabel: first ? sourceLabel(first.source, t) : t('access.sources.unknown'),
      permissions: rows,
      risk: first?.source === 'team_member' ? t('access.risks.teamRoleRuntime') : null,
    };
  });
}

function compactPermissions(rows: EffectivePermission[]): string {
  return rows.map((p) => p.key).join(', ');
}

function EmptyLine({ children, testId }: { children: React.ReactNode; testId: string }): React.ReactElement {
  return (
    <p className="py-2 text-xs text-text-muted" data-testid={testId}>
      {children}
    </p>
  );
}

function PermissionPill({ permission }: { permission: string }): React.ReactElement {
  return (
    <span className="inline-flex max-w-full rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.6875rem] text-text-primary">
      <span className="min-w-0 truncate">{permission}</span>
    </span>
  );
}

function Tag({ children, testId }: { children: React.ReactNode; testId?: string }): React.ReactElement {
  return (
    <span
      className="inline-flex max-w-full rounded border border-border-base bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-medium text-text-secondary"
      data-testid={testId}
    >
      <span className="min-w-0 truncate">{children}</span>
    </span>
  );
}

function CompletenessNotice({
  kind,
  state,
  warnings,
}: {
  kind: 'effective' | 'audit';
  state: 'complete' | 'truncated' | 'unknown';
  warnings?: string[];
}): React.ReactElement {
  const { t } = useTranslation('members');
  if (state === 'complete' && (!warnings || warnings.length === 0)) {
    return (
      <p className="mt-2 rounded border border-success/30 bg-success/10 px-3 py-2 text-xs text-success" data-testid={`access-${kind}-complete`}>
        {t(`access.completeness.${kind}.complete`)}
      </p>
    );
  }
  const text = state === 'truncated'
    ? t(`access.completeness.${kind}.truncated`)
    : t(`access.completeness.${kind}.unknown`);
  return (
    <div
      className="mt-2 rounded border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning"
      data-testid={`access-${kind}-${state === 'truncated' ? 'truncated' : 'partial'}`}
      role="status"
    >
      <p>{text}</p>
      {warnings && warnings.length > 0 && (
        <ul className="mt-1 list-disc space-y-0.5 pl-4">
          {warnings.map((warning) => <li key={warning}>{warning}</li>)}
        </ul>
      )}
    </div>
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
  const detail = sourceDetail(permission, resource, subjectRef, t);
  return (
    <span className="flex min-w-0 flex-col gap-1">
      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="font-medium text-text-primary">{detail.label}</span>
        <span className="min-w-0 truncate font-mono text-[0.6875rem] text-text-muted" title={evidenceText(permission)}>
          {evidenceText(permission)}
        </span>
      </span>
      <span className="text-xs text-text-muted">{detail.fact}</span>
    </span>
  );
}

function PermissionRows({
  rows,
  resource,
  subjectRef,
  empty,
  testId,
}: {
  rows: EffectivePermission[];
  resource: ResourceScope;
  subjectRef: string;
  empty: string;
  testId: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  if (rows.length === 0) {
    return <EmptyLine testId={`${testId}-empty`}>{empty}</EmptyLine>;
  }
  return (
    <ul className="divide-y divide-border-base" data-testid={testId}>
      {rows.map((p) => {
        const teamRole = teamRoleFromEvidence(p.evidence_ref);
        return (
          <li
            key={`${p.key}:${p.source}:${p.evidence_ref}:${p.assignment_id ?? ''}`}
            className="grid grid-cols-1 gap-2 py-2 text-sm md:grid-cols-[minmax(12rem,1fr)_minmax(16rem,1.4fr)_minmax(9rem,0.8fr)]"
            data-testid="access-permission-row"
            data-permission-key={p.key}
            data-source={p.source}
          >
            <span className="min-w-0">
              <PermissionPill permission={p.key} />
            </span>
            <SourceCell permission={p} resource={resource} subjectRef={subjectRef} />
            <span className="flex min-w-0 flex-wrap items-start gap-1">
              <Tag testId="access-role-chip">{t('access.roles.accessRoleChip', { role: accessRoleLabel(p, t) })}</Tag>
              {teamRole && (
                <Tag testId="access-combination-chip">
                  {t('access.roles.combinationChip', { teamRole, accessRole: accessRoleLabel(p, t) })}
                </Tag>
              )}
              <Tag>{p.delegatable ? t('access.roles.delegatable') : t('access.roles.fixed')}</Tag>
            </span>
          </li>
        );
      })}
    </ul>
  );
}

function DrillDown({
  subjectRef,
  subjectLabel,
  resource,
  resourceLabel,
  resolvedOrg,
}: {
  subjectRef: string;
  subjectLabel: string;
  resource: ResourceScope;
  resourceLabel: string;
  resolvedOrg?: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const fields: Array<[string, string]> = [
    [t('access.subject.label'), subjectLabel],
    [t('access.subject.ref'), subjectRef],
    [t('access.subject.kindLabel'), subjectKind(subjectRef, t)],
    [t('access.resource.label'), resourceLabel || resourceText(resource)],
    [t('access.resource.kind'), resource.kind],
    [t('access.resource.id'), resource.uri || resource.id || resource.org_id || '*'],
    [t('access.resource.org'), resolvedOrg || resource.org_id || resource.id || 'unknown'],
  ];
  return (
    <section
      className="rounded border border-border-base bg-bg-elevated p-4"
      data-testid="access-drilldown"
      role="region"
      aria-label={t('access.drilldown.aria')}
    >
      <h3 className="text-sm font-semibold text-text-primary">{t('access.drilldown.title')}</h3>
      <dl className="mt-3 grid grid-cols-1 gap-2 text-sm md:grid-cols-[8rem_1fr]">
        {fields.map(([label, value], index) => (
          <div key={`${index}:${label}`} className="contents">
            <dt className="text-xs text-text-muted">{label}</dt>
            <dd className="min-w-0 truncate font-mono text-xs text-text-primary" title={value}>
              {value}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function AccessGraph({
  edges,
  subjectLabel,
  subjectRef,
  resource,
}: {
  edges: AccessGraphEdge[];
  subjectLabel: string;
  subjectRef: string;
  resource: ResourceScope;
}): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <section
      className="rounded border border-border-base bg-bg-elevated p-4"
      data-testid="access-graph"
      role="region"
      aria-label={t('access.graph.aria')}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">{t('access.graph.title')}</h3>
          <p className="mt-1 text-xs text-text-muted">{t('access.graph.subtitle')}</p>
        </div>
        <Tag>{t('access.readonly.mode')}</Tag>
      </div>
      {edges.length === 0 ? (
        <EmptyLine testId="access-graph-empty">{t('access.graph.empty')}</EmptyLine>
      ) : (
        <ol className="mt-3 space-y-2" data-testid="access-graph-list">
          {edges.map((edge) => (
            <li
              key={edge.id}
              className="grid grid-cols-1 gap-2 rounded border border-border-base bg-bg-subtle p-3 text-xs lg:grid-cols-[minmax(9rem,1fr)_1.25rem_minmax(10rem,1fr)_1.25rem_minmax(12rem,1.2fr)_1.25rem_minmax(9rem,1fr)]"
              data-testid="access-graph-edge"
            >
              <GraphNode label={t('access.graph.subject')} title={subjectLabel} detail={subjectRef} />
              <GraphArrow />
              <GraphNode label={t('access.graph.source')} title={edge.sourceLabel} detail={edge.source} />
              <GraphArrow />
              <GraphNode label={t('access.graph.permissions')} title={String(edge.permissions.length)} detail={compactPermissions(edge.permissions)} />
              <GraphArrow />
              <GraphNode label={t('access.graph.resource')} title={resource.kind} detail={resourceText(resource)} />
              {edge.risk && (
                <p className="lg:col-span-7 rounded border border-warning/30 bg-warning/10 px-2 py-1 text-warning" data-testid="access-graph-risk">
                  {edge.risk}
                </p>
              )}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function GraphNode({ label, title, detail }: { label: string; title: string; detail: string }): React.ReactElement {
  return (
    <span className="min-w-0">
      <span className="block text-[0.6875rem] uppercase text-text-muted">{label}</span>
      <span className="block truncate font-semibold text-text-primary" title={title}>{title}</span>
      <span className="block truncate font-mono text-[0.6875rem] text-text-muted" title={detail}>{detail}</span>
    </span>
  );
}

function GraphArrow(): React.ReactElement {
  return <span className="hidden self-center text-center text-text-muted lg:block" aria-hidden="true">-&gt;</span>;
}

function SourceLocations({
  rows,
  resource,
  subjectRef,
}: {
  rows: EffectivePermission[];
  resource: ResourceScope;
  subjectRef: string;
}): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <section
      className="rounded border border-border-base bg-bg-elevated p-4"
      data-testid="access-source-locations"
      role="region"
      aria-label={t('access.sourceLocations.aria')}
    >
      <h3 className="text-sm font-semibold text-text-primary">{t('access.sourceLocations.title')}</h3>
      {rows.length === 0 ? (
        <EmptyLine testId="access-source-locations-empty">{t('access.sourceLocations.empty')}</EmptyLine>
      ) : (
        <ul className="mt-3 divide-y divide-border-base" data-testid="access-source-locations-list">
          {rows.map((p) => {
            const detail = sourceDetail(p, resource, subjectRef, t);
            return (
              <li
                key={`${p.key}:${p.source}:${p.evidence_ref}:${p.assignment_id ?? ''}`}
                className="grid grid-cols-1 gap-2 py-2 text-sm lg:grid-cols-[minmax(11rem,1fr)_minmax(13rem,1.1fr)_minmax(13rem,1.1fr)_minmax(10rem,0.9fr)]"
                data-testid="access-source-location-row"
              >
                <span className="min-w-0">
                  <PermissionPill permission={p.key} />
                </span>
                <span className="min-w-0">
                  <span className="block font-medium text-text-primary">{detail.label}</span>
                  <span className="block truncate font-mono text-[0.6875rem] text-text-muted" title={evidenceText(p)}>
                    {evidenceText(p)}
                  </span>
                </span>
                <span className="min-w-0 text-xs text-text-secondary">
                  {detail.editLocation}
                  {detail.href && (
                    <>
                      {' '}
                      <OrgLink
                        to={detail.href}
                        className="text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                        data-testid="access-source-link"
                      >
                        {t('access.source.openLocation')}
                      </OrgLink>
                    </>
                  )}
                </span>
                <span className="min-w-0 text-xs text-text-muted" data-testid="access-source-risk">
                  {detail.risk ?? t('access.risks.none')}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function RoleModel({
  permissions,
}: {
  permissions: EffectivePermission[];
}): React.ReactElement {
  const { t } = useTranslation('members');
  const teamRoles = [...new Set(permissions.map((p) => teamRoleFromEvidence(p.evidence_ref)).filter((v): v is string => !!v))];
  const accessRoles = [...new Set(permissions.map((p) => accessRoleLabel(p, t)))];
  const combinations = permissions
    .map((p) => {
      const teamRole = teamRoleFromEvidence(p.evidence_ref);
      return teamRole ? `${teamRole}+${accessRoleLabel(p, t)}` : null;
    })
    .filter((v): v is string => !!v);
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-role-model">
      <h3 className="text-sm font-semibold text-text-primary">{t('access.roles.title')}</h3>
      <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
        <RoleColumn
          title={t('access.roles.teamRoleTitle')}
          body={t('access.roles.teamRoleBody')}
          empty={t('access.roles.teamRoleEmpty')}
          items={teamRoles}
          testId="access-team-roles"
        />
        <RoleColumn
          title={t('access.roles.accessRoleTitle')}
          body={t('access.roles.accessRoleBody')}
          empty={t('access.roles.accessRoleEmpty')}
          items={accessRoles}
          testId="access-access-roles"
        />
      </div>
      <div className="mt-3 rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary">
        <p className="font-medium text-text-primary">{t('access.roles.combinationTitle')}</p>
        <p className="mt-1">{t('access.roles.combinationBody')}</p>
        {combinations.length > 0 ? (
          <div className="mt-2 flex flex-wrap gap-1" data-testid="access-combination-tags">
            {[...new Set(combinations)].map((combo) => {
              const [teamRole, accessRole] = combo.split('+');
              return (
                <Tag key={combo} testId="access-combination-tag">
                  {t('access.roles.combinationChip', { teamRole, accessRole })}
                </Tag>
              );
            })}
          </div>
        ) : (
          <EmptyLine testId="access-combination-empty">{t('access.roles.combinationEmpty')}</EmptyLine>
        )}
      </div>
    </section>
  );
}

function RoleColumn({
  title,
  body,
  empty,
  items,
  testId,
}: {
  title: string;
  body: string;
  empty: string;
  items: string[];
  testId: string;
}): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-subtle px-3 py-2" data-testid={testId}>
      <h4 className="text-xs font-semibold uppercase text-text-muted">{title}</h4>
      <p className="mt-1 text-xs text-text-secondary">{body}</p>
      {items.length === 0 ? (
        <p className="mt-2 text-xs text-text-muted">{empty}</p>
      ) : (
        <div className="mt-2 flex flex-wrap gap-1">
          {items.map((item) => <Tag key={item}>{item}</Tag>)}
        </div>
      )}
    </div>
  );
}

function RiskPanel({
  risks,
}: {
  risks: string[];
}): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <section
      className="rounded border border-warning/30 bg-warning/10 p-4 text-warning"
      data-testid="access-risk-panel"
      role="region"
      aria-label={t('access.risks.aria')}
    >
      <h3 className="text-sm font-semibold">{t('access.risks.title')}</h3>
      {risks.length === 0 ? (
        <p className="mt-2 text-xs" data-testid="access-risk-empty">{t('access.risks.empty')}</p>
      ) : (
        <ul className="mt-2 list-disc space-y-1 pl-4 text-xs" data-testid="access-risk-list">
          {risks.map((risk) => <li key={risk}>{risk}</li>)}
        </ul>
      )}
    </section>
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
  const [selectedPermission, setSelectedPermission] = useState('');

  const permissions = effective.data?.permissions ?? [];
  const accessRolePermissions = permissions.filter((p) => p.source === CUSTOM_ROLE_SOURCE);
  const legacyPermissions = permissions.filter((p) => p.source !== CUSTOM_ROLE_SOURCE);
  const candidatePermissions = useMemo(() => {
    const keys = new Set(definitionKeysForResource(definitions.data?.definitions, resource));
    for (const p of permissions) keys.add(p.key);
    return [...keys].sort();
  }, [definitions.data?.definitions, permissions, resource]);
  const selected = candidatePermissions.includes(selectedPermission)
    ? selectedPermission
    : candidatePermissions[0] ?? '';
  const explain = usePermissionExplain(subjectRef, selected, resource, !!selected);
  const resolvedResource: ResourceScope = effective.data?.resource ?? explain.data?.decision.resource ?? resource ?? { kind: 'org' };
  const auditEvents = audit.data?.events ?? [];
  const effectiveCompleteness = completenessState(effective.data);
  const auditCompleteness = completenessState(audit.data);
  const edges = useMemo(() => graphEdges(permissions, t), [permissions, t]);
  const risks = useMemo(() => {
    const out = new Set<string>();
    if (effectiveCompleteness !== 'complete') out.add(t('access.completeness.effective.unknown'));
    if (auditCompleteness !== 'complete') out.add(t('access.completeness.audit.unknown'));
    if (permissions.some((p) => p.source === 'team_member')) out.add(t('access.risks.teamRoleRuntime'));
    if (permissions.some((p) => p.source === 'worker_owner' || p.source === 'agent_worker_binding')) {
      out.add(t('access.risks.runtimeBinding'));
    }
    if ((explain.data?.denied_by ?? []).length > 0) out.add(t('access.risks.deniedExplain'));
    for (const warning of effective.data?.warnings ?? []) out.add(warning);
    for (const warning of audit.data?.warnings ?? []) out.add(warning);
    return [...out];
  }, [
    audit.data?.warnings,
    auditCompleteness,
    effective.data?.warnings,
    effectiveCompleteness,
    explain.data?.denied_by,
    permissions,
    t,
  ]);

  if (!resource) {
    return (
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-permissions-panel">
        <EmptyLine testId="access-unavailable">{t('access.unavailable')}</EmptyLine>
      </section>
    );
  }

  return (
    <section
      className="space-y-4"
      data-testid="access-permissions-panel"
      data-subject-ref={subjectRef}
      aria-busy={effective.isLoading || definitions.isLoading || audit.isLoading}
    >
      <section
        className="rounded border border-border-base bg-bg-elevated p-4"
        data-testid="access-overview"
        role="region"
        aria-label={t('access.overview.aria')}
      >
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
        <p className="mt-3 rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="access-readonly-banner" role="status">
          {t('access.readonly.banner')}
        </p>
        <dl className="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <Metric label={t('access.overview.total')} value={String(permissions.length)} testId="access-overview-total" />
          <Metric label={t('access.overview.accessRoles')} value={String(accessRolePermissions.length)} testId="access-overview-access-roles" />
          <Metric label={t('access.overview.legacySources')} value={String(legacyPermissions.length)} testId="access-overview-legacy" />
          <Metric label={t('access.overview.risks')} value={String(risks.length)} testId="access-overview-risks" />
        </dl>
        {effective.isLoading && <EmptyLine testId="access-loading">{t('access.loading')}</EmptyLine>}
        {effective.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="access-error" role="alert">
            {(effective.error as Error).message}
          </p>
        )}
        {definitions.isError && (
          <p className="mt-3 text-xs text-warning" data-testid="access-definitions-error" role="status">
            {t('access.definitionsError')}
          </p>
        )}
        {!effective.isLoading && !effective.isError && permissions.length === 0 && (
          <EmptyLine testId="access-effective-empty">{t('access.effective.empty')}</EmptyLine>
        )}
        <CompletenessNotice kind="effective" state={effectiveCompleteness} warnings={effective.data?.warnings} />
      </section>

      <DrillDown
        subjectRef={subjectRef}
        subjectLabel={subjectLabel}
        resource={resolvedResource}
        resourceLabel={resourceLabel}
        resolvedOrg={explain.data?.resolved_org}
      />

      <RiskPanel risks={risks} />

      <RoleModel permissions={permissions} />

      <AccessGraph
        edges={edges}
        subjectLabel={subjectLabel}
        subjectRef={subjectRef}
        resource={resolvedResource}
      />

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-effective">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.effective.title')}</h3>
        <PermissionRows
          rows={permissions}
          resource={resolvedResource}
          subjectRef={subjectRef}
          empty={t('access.effective.empty')}
          testId="access-effective-list"
        />
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-explain-tree">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">{t('access.explain.title')}</h3>
            <p className="mt-1 text-xs text-text-muted">{t('access.explain.subtitle')}</p>
          </div>
          <label className="flex min-w-0 flex-col gap-1 text-xs text-text-muted" htmlFor="access-explain-select">
            {t('access.explain.permissionLabel')}
            <select
              id="access-explain-select"
              value={selected}
              onChange={(e) => setSelectedPermission(e.target.value)}
              className="min-h-[36px] min-w-0 rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary"
              data-testid="access-explain-select"
              disabled={candidatePermissions.length === 0}
            >
              {candidatePermissions.length === 0 ? (
                <option value="">{t('access.explain.noPermissions')}</option>
              ) : (
                candidatePermissions.map((key) => (
                  <option key={key} value={key}>
                    {key}
                  </option>
                ))
              )}
            </select>
          </label>
        </div>
        {explain.isLoading && <EmptyLine testId="access-explain-loading">{t('access.loading')}</EmptyLine>}
        {explain.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="access-explain-error" role="alert">
            {(explain.error as Error).message}
          </p>
        )}
        {explain.data && (
          <div className="mt-3 space-y-2 text-sm">
            <div className="grid grid-cols-1 gap-2 md:grid-cols-[8rem_1fr]">
              <span className="text-text-muted">{t('access.explain.subject')}</span>
              <span className="min-w-0 truncate font-mono text-text-primary">{explain.data.decision.subject_ref}</span>
              <span className="text-text-muted">{t('access.explain.resource')}</span>
              <span className="min-w-0 truncate font-mono text-text-primary">{resourceText(explain.data.decision.resource)}</span>
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
            {explain.data.effective.length > 0 ? (
              <ul className="divide-y divide-border-base" data-testid="access-explain-sources">
                {explain.data.effective.map((p) => (
                  <li key={`${p.key}:${p.source}:${p.evidence_ref}`} className="flex min-w-0 flex-wrap items-center gap-2 py-1.5">
                    <PermissionPill permission={p.key} />
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

      <SourceLocations rows={permissions} resource={resolvedResource} subjectRef={subjectRef} />

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-audit">
        <h3 className="text-sm font-semibold text-text-primary">{t('access.audit.title')}</h3>
        <CompletenessNotice kind="audit" state={auditCompleteness} warnings={audit.data?.warnings} />
        {audit.isLoading && <EmptyLine testId="access-audit-loading">{t('access.loading')}</EmptyLine>}
        {audit.isError && (
          <p className="mt-2 text-xs text-danger" data-testid="access-audit-error" role="alert">
            {(audit.error as Error).message}
          </p>
        )}
        {!audit.isLoading && auditEvents.length === 0 && (
          <EmptyLine testId="access-audit-empty">{t('access.audit.empty')}</EmptyLine>
        )}
        {auditEvents.length > 0 && (
          <ul className="mt-2 divide-y divide-border-base" data-testid="access-audit-list">
            {auditEvents.map((e) => (
              <li key={e.id} className="grid grid-cols-1 gap-2 py-2 text-sm md:grid-cols-[8rem_1fr]" data-testid="access-audit-row">
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

function Metric({ label, value, testId }: { label: string; value: string; testId: string }): React.ReactElement {
  return (
    <div>
      <dt className="text-xs text-text-muted">{label}</dt>
      <dd className="text-lg font-semibold text-text-primary" data-testid={testId}>
        {value}
      </dd>
    </div>
  );
}
