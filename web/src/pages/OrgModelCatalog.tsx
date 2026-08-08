import type React from 'react';
import { Navigate, useParams } from 'react-router-dom';

// Compatibility shim: the old Workspace > Model catalog UI has converged into
// AI Runtime > Models so model management writes through one catalog surface.
export default function OrgModelCatalog(): React.ReactElement {
  const { slug } = useParams<{ slug?: string }>();
  return <Navigate to={slug ? `/organizations/${slug}/ai-runtime?tab=models` : '/ai-runtime?tab=models'} replace />;
}
