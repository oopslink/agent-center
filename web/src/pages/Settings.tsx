import type React from 'react';
import { useTranslation } from 'react-i18next';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { WakeGuardrailPanel } from '@/components/WakeGuardrailPanel';
import { LanguagePanel } from '@/components/LanguagePanel';
import { useSystemSegments } from './useSystemSegments';

// Settings page. I7-D3: the build/version-identity panel moved out to its own
// System-level /version page (sibling to Environment / Settings); this page now
// hosts the wake-chain guardrail params panel (MAX_DEPTH / cycle window+N /
// rate R·min⁻¹ / token budget), backed by D1's live settings API (I7-M1).
// v2.25 F0: also hosts the Language panel (zh/en switch).
export default function Settings(): React.ReactElement {
  const { t } = useTranslation('common');
  const systemSegments = useSystemSegments();
  return (
    <section className="space-y-4" data-testid="page-Settings">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings | Version) — desktop keeps the rail's col② nav. */}
      <SegmentedNav items={systemSegments.segments} ariaLabel={systemSegments.ariaLabel} />
      <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
      <LanguagePanel />
      <WakeGuardrailPanel />
    </section>
  );
}
