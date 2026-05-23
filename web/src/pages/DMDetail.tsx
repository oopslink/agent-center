import type React from 'react';

// DMDetail page — F3 placeholder. Real implementation lands in a later ST
// (see phase-11-frontend-plan.md § 7). Kept intentionally minimal so the
// route tree + lazy split can ship without business code.
export default function DMDetail(): React.ReactElement {
  return (
    <section className="space-y-2" data-testid="page-DMDetail">
      <h2 className="text-xl font-semibold">DMDetail</h2>
      <p className="text-sm text-slate-500">F3 placeholder — replaced in a later ST.</p>
    </section>
  );
}
