import type { Segment } from '@/shell/SegmentedNav';

// v2.10.1 [M7] The System module's mobile "二级段控" segments — Environment |
// Settings | Version. Shared by the System pages so the mobile SegmentedNav stays
// in sync (I7-D3 added Version as a sibling System-level page).
export const SYSTEM_SEGMENTS: ReadonlyArray<Segment> = [
  { label: 'Environment', to: '/environment', testId: 'system-seg-environment' },
  { label: 'Settings', to: '/settings', testId: 'system-seg-settings' },
  { label: 'Version', to: '/version', testId: 'system-seg-version' },
];
