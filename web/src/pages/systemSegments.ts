import type { Segment } from '@/shell/SegmentedNav';

// v2.10.1 [M7] The System module's mobile "二级段控" segments — Environment |
// Settings. Shared by both pages so the mobile SegmentedNav stays in sync.
export const SYSTEM_SEGMENTS: ReadonlyArray<Segment> = [
  { label: 'Environment', to: '/environment', testId: 'system-seg-environment' },
  { label: 'Settings', to: '/settings', testId: 'system-seg-settings' },
];
