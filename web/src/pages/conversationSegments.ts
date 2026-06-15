import type { Segment } from '@/shell/SegmentedNav';

// v2.10.2 [T129] The Conversations module's mobile "二级段控" segments —
// Channels | DMs. Shared by both list pages so the mobile SegmentedNav stays in
// sync (mirrors systemSegments.ts). On desktop the col② secondary nav owns this
// Channels/DMs switch; col② is md:hidden on mobile, so each list page surfaces
// this SegmentedNav instead — fixing T129 (mobile Chat showed only Channels with
// no way to reach DMs).
export const CONVERSATION_SEGMENTS: ReadonlyArray<Segment> = [
  { label: 'Channels', to: '/channels', testId: 'conv-seg-channels' },
  { label: 'DMs', to: '/dms', testId: 'conv-seg-dms' },
];
