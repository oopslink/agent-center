import type React from 'react';

// Fleet page — F3 placeholder. Real implementation lands in a later ST
// (see phase-11-frontend-plan.md § 7). Kept intentionally minimal so the
// route tree + lazy split can ship without business code.
export default function Fleet(): React.ReactElement {
  return (
    <section className="space-y-2" data-testid="page-Fleet">
      <h2 className="text-xl font-semibold">Fleet</h2>
      <p className="text-sm text-slate-500">F3 placeholder — replaced in a later ST.</p>
    </section>
  );
}
