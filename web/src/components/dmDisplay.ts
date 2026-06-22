import type { Conversation } from '@/api/types';

// dmDisplayName — the human label for an agent↔agent DM: the backend dm_title
// when present, else "@A ↔ @B" built from dm_participants. Shared by the DMs page
// and the col② Conversations nav (T308) so both name agent-agent DMs identically.
// Falls back to "Direct message".
export function dmDisplayName(c: Conversation): string {
  if (c.dm_title) return c.dm_title;
  if (c.dm_type === 'agent_agent_dm' && c.dm_participants?.length) {
    return c.dm_participants
      .map((p) => (p.display_name ? `@${p.display_name}` : p.identity_id))
      .join(' ↔ ');
  }
  return 'Direct message';
}
