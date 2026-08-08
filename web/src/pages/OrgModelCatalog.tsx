import type React from 'react';
import { Navigate } from 'react-router-dom';

// Compatibility component for the retired Workspace > Model catalog page.
// AI Runtime is now the single UI surface and source of truth for model entries.
export default function OrgModelCatalog(): React.ReactElement {
  return <Navigate to="../ai-runtime" replace relative="path" />;
}
