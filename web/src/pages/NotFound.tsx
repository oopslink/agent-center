import type React from 'react';
import { OrgLink } from '@/OrgContext';


// NotFound — 404 with a navigation link back to the home so users don't
// get stuck. Per x9527 #6 open question 5.
export default function NotFound(): React.ReactElement {
  return (
    <section className="space-y-4" data-testid="page-NotFound">
      <h2 className="text-xl font-semibold">404 — Not found</h2>
      <p className="text-sm text-text-muted">
        The page you requested does not exist.
      </p>
      <div className="space-x-3">
        <OrgLink
          to="/"
          className="text-accent hover:underline"
          data-testid="nav-home"
        >
          Back to home
        </OrgLink>
      </div>
    </section>
  );
}
