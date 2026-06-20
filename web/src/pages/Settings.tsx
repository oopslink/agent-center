import type React from 'react';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { WakeGuardrailPanel } from '@/components/WakeGuardrailPanel';
import { SYSTEM_SEGMENTS } from './systemSegments';

// Settings page. I7-D3: the build/version-identity panel moved out to its own
// System-level /version page (sibling to Environment / Settings); this page now
// hosts the wake-chain guardrail params panel (MAX_DEPTH / cycle window+N /
// rate R·min⁻¹ / token budget), backed by D1's live settings API (I7-M1).
export default function Settings(): React.ReactElement {
  return (
    <section className="space-y-4" data-testid="page-Settings">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings | Version) — desktop keeps the rail's col② nav. */}
      <SegmentedNav items={SYSTEM_SEGMENTS} ariaLabel="System sections" />
      <h1 className="text-xl font-semibold">Settings</h1>
      <WakeGuardrailPanel />
    </section>
  );
}
