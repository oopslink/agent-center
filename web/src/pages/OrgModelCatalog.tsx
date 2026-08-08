import type React from 'react';
import { Navigate } from 'react-router-dom';
import { orgPath, useOptionalOrgContext } from '@/OrgContext';

// Compatibility shim for the retired Workspace > Model catalog surface.
// AI Runtime is the only frontend catalog editor; stale imports and direct links
// are redirected to the canonical System > AI Runtime page.
export default function OrgModelCatalog(): React.ReactElement {
  const org = useOptionalOrgContext();
  return <Navigate to={orgPath('/ai-runtime', org?.slug)} replace />;
}
