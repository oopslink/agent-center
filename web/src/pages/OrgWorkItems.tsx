import type React from 'react';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useOrgWorkItems, type OrgWorkItemKind } from '@/api/orgWorkItems';
import { OrgWorkItemsView } from '@/components/OrgWorkItemsView';
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
  const [createOpen, setCreateOpen] = useState(false);
  // empty selection = omit status (backend default excludes terminal states);
  // selected = pass the chosen statuses.
  const filters = selectedStatuses.length > 0 ? { status: selectedStatuses } : undefined;
  const query = useOrgWorkItems(kind, slug, filters);

  return (
    <>
      <OrgWorkItemsView
        kind={kind}
        query={query}
        selectedStatuses={selectedStatuses}
        onStatusesChange={setSelectedStatuses}
        onCreate={() => setCreateOpen(true)}
      />
      {createOpen && <OrgWorkItemCreateModal kind={kind} onClose={() => setCreateOpen(false)} />}
    </>
  );
}
