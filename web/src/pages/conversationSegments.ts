import type { Segment } from '@/shell/SegmentedNav';

// v2.10.2 [T129] The Conversations module's mobile "二级段控" segments —
// Channels | DMs. Shared by both list pages so the mobile SegmentedNav stays in
// sync (mirrors systemSegments.ts). On desktop the col② secondary nav owns this
// Channels/DMs switch; col② is md:hidden on mobile, so each list page surfaces
// this SegmentedNav instead — fixing T129 (mobile Chat showed only Channels with
// no way to reach DMs).
// T343: "Unread" is the first mobile segment — the cross-source unread digest
// (desktop has it in col②; mobile had no way to reach it → @oopslink "移动端 chat
// 缺少 unread 消息"). Channels / DMs follow.
export const CONVERSATION_SEGMENTS: ReadonlyArray<Segment> = [
  { label: 'Unread', to: '/unread', testId: 'conv-seg-unread' },
  { label: 'Channels', to: '/channels', testId: 'conv-seg-channels' },
  { label: 'DMs', to: '/dms', testId: 'conv-seg-dms' },
];
