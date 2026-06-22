import type { Conversation } from '@/api/types';

// dmDisplayName — the human label for an agent↔agent DM: the backend dm_title
// when present, else "@A ↔ @B" built from dm_participants. Shared by the DMs page
// and the col② Conversations nav (T308) so both name agent-agent DMs identically.
// Falls back to "Direct message".
export function dmDisplayName(c: Conversation): string {
  if (c.dm_title) return c.dm_title;
  if (c.dm_type === 'agent_agent_dm' && c.dm_participants?.length) {
    return dmParticipantLabels(c).join(' ↔ ');
  }
  return 'Direct message';
}

// dmParticipantLabels — the per-participant labels ("@name", or the raw
// identity_id when unnamed) of an agent↔agent DM, in order. T318: the col② nav
// stacks these on separate lines so both agents stay legible in the narrow rail
// (the single-line "@A ↔ @B" truncated the second agent). Empty for a non-
// agent-agent DM.
export function dmParticipantLabels(c: Conversation): string[] {
  if (c.dm_type === 'agent_agent_dm' && c.dm_participants?.length) {
    return c.dm_participants.map((p) => (p.display_name ? `@${p.display_name}` : p.identity_id));
  }
  return [];
}
