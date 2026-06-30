import { useTranslation } from 'react-i18next';
import type { Segment } from '@/shell/SegmentedNav';

// Stable ids + routes for the System module's mobile "二级段控" segments. The
// id/to/testId literals are byte-stable (routing + data-testid contract); only
// the display label is localised at render time via t('systemNav.<id>').
const SYSTEM_SEGMENT_DEFS: ReadonlyArray<{ id: string; to: string; testId: string }> = [
  { id: 'environment', to: '/environment', testId: 'system-seg-environment' },
  { id: 'settings', to: '/settings', testId: 'system-seg-settings' },
  { id: 'version', to: '/version', testId: 'system-seg-version' },
];

// Returns the localised System segments (same to + testId, label via
// t('systemNav.<id>')) plus the localised SegmentedNav aria label.
export function useSystemSegments(): { segments: ReadonlyArray<Segment>; ariaLabel: string } {
  const { t } = useTranslation('admin');
  const segments = SYSTEM_SEGMENT_DEFS.map((s) => ({
    label: t(`systemNav.${s.id}`),
    to: s.to,
    testId: s.testId,
  }));
  return { segments, ariaLabel: t('systemNav.aria') };
}
