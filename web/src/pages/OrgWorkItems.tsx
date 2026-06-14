import type React from 'react';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useOrgWorkItems, type OrgWorkItemFilters, type OrgWorkItemKind } from '@/api/orgWorkItems';
import { OrgWorkItemsView, type DateRange } from '@/components/OrgWorkItemsView';
import { OrgWorkItemCreateModal } from '@/components/OrgWorkItemCreateModal';
import { localDateToRFC3339 } from '@/utils/time';

// OrgWorkItems (v2.8 #258) — org-scoped cross-project Issues / Tasks aggregation
// page (/organizations/:slug/issues|tasks). One component, two routes via the
// `kind` prop.
//
// 6th task: a status FilterBar (multi-select; empty = backend default all-open),
// a Created column, and a cross-project Create button (→ project-picker modal).
export default function OrgWorkItemsPage({ kind }: { kind: OrgWorkItemKind }): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>([]);
  // project filter (multi) = array of project ids; assignee filter (single) =
  // a prefixed identity ref ("user:<id>" / "agent:<id>"), '' = Any.
  const [selectedProjects, setSelectedProjects] = useState<string[]>([]);
  const [assignee, setAssignee] = useState<string>('');
  // #258 date-range filters: raw "YYYY-MM-DD" picker values (the FilterBar's
  // local state). Converted to RFC3339-with-LOCAL-offset only when calling the
  // hook (see localDateToRFC3339 — the off-by-one 命门: never send naive/UTC).
  const [dateRange, setDateRange] = useState<DateRange>({
    created_after: '',
    created_before: '',
    updated_after: '',
    updated_before: '',
  });
  const [createOpen, setCreateOpen] = useState(false);
  // v2.10.0 [T3]: the selected row → drives the col④ read-only metadata panel.
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Build filters. empty status selection = omit status (backend default excludes
  // terminal states). Each date param is converted local→RFC3339-offset (start =
  // 00:00:00, end/"before" = 23:59:59) and omitted when its picker is empty.
  const filters: OrgWorkItemFilters = {};
  if (selectedStatuses.length > 0) filters.status = selectedStatuses;
  // project = multi (repeated `project=<id>`, OR semantics backend-side);
  // assignee = single (prefixed identity ref). Omitted when unset.
  if (selectedProjects.length > 0) filters.project = selectedProjects;
  if (assignee) filters.assignee = assignee;
  const createdAfter = localDateToRFC3339(dateRange.created_after, 'start');
  const createdBefore = localDateToRFC3339(dateRange.created_before, 'end');
  const updatedAfter = localDateToRFC3339(dateRange.updated_after, 'start');
  const updatedBefore = localDateToRFC3339(dateRange.updated_before, 'end');
  if (createdAfter) filters.created_after = createdAfter;
  if (createdBefore) filters.created_before = createdBefore;
  if (updatedAfter) filters.updated_after = updatedAfter;
  if (updatedBefore) filters.updated_before = updatedBefore;
  const hasFilters = Object.keys(filters).length > 0;
  const query = useOrgWorkItems(kind, slug, hasFilters ? filters : undefined);

  return (
    <>
      <OrgWorkItemsView
        kind={kind}
        query={query}
        selectedStatuses={selectedStatuses}
        onStatusesChange={setSelectedStatuses}
        selectedProjects={selectedProjects}
        onProjectsChange={setSelectedProjects}
        assignee={assignee}
        onAssigneeChange={setAssignee}
        dateRange={dateRange}
        onDateRangeChange={setDateRange}
        onCreate={() => setCreateOpen(true)}
        selectedId={selectedId}
        onSelect={setSelectedId}
      />
      {createOpen && <OrgWorkItemCreateModal kind={kind} onClose={() => setCreateOpen(false)} />}
    </>
  );
}
