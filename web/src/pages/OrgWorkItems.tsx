import type React from 'react';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useOrgWorkItems, buildWorkItemFilters, type OrgWorkItemKind } from '@/api/orgWorkItems';
import { OrgWorkItemsView } from '@/components/OrgWorkItemsView';
import { type DateRange } from '@/components/WorkItemFilterBar';
import { OrgWorkItemCreateModal } from '@/components/OrgWorkItemCreateModal';

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

  // Build the wire filters from the FilterBar state (shared helper — the
  // local-date→RFC3339-offset 命门 lives there). undefined when nothing is set.
  const filters = buildWorkItemFilters({ selectedStatuses, selectedProjects, assignee, dateRange });
  const query = useOrgWorkItems(kind, slug, filters);

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
