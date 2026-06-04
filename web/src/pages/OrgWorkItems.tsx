import type React from 'react';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useOrgWorkItems, type OrgWorkItemKind } from '@/api/orgWorkItems';
import { OrgWorkItemsView } from '@/components/OrgWorkItemsView';

// OrgWorkItems (v2.8 #258) — org-scoped cross-project Issues / Tasks aggregation
// page (/organizations/:slug/issues|tasks). One component, two routes via the
// `kind` prop. Default view = open only (backend "all open"); the toggle drops
// the status filter by passing the full status set.
const ALL_STATUS: Record<OrgWorkItemKind, string[]> = {
  issue: ['open', 'in_progress', 'resolved', 'closed', 'withdrawn', 'reopened'],
  task: ['open', 'assigned', 'running', 'blocked', 'completed', 'verified', 'canceled', 'reopened'],
};

export default function OrgWorkItemsPage({ kind }: { kind: OrgWorkItemKind }): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const [openOnly, setOpenOnly] = useState(true);
  // open-only = omit status (backend default excludes terminal); All = pass full set.
  const filters = openOnly ? undefined : { status: ALL_STATUS[kind] };
  const query = useOrgWorkItems(kind, slug, filters);

  return (
    <OrgWorkItemsView kind={kind} query={query} openOnly={openOnly} onOpenOnlyChange={setOpenOnly} />
  );
}
