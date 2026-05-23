import type React from 'react';
import { useState } from 'react';
import { DeriveBar } from './DeriveBar';
import { DeriveModal, type DeriveKind } from './DeriveModal';
import type { SelectionState } from './useSelection';

interface Props {
  conversationId: string;
  selection: SelectionState;
}

// ConversationDeriveControls — bundles the bottom DeriveBar + the modal,
// driven by a useSelection() instance owned by the parent page. The
// page renders this anywhere inside the conversation surface; on
// success the selection clears so the rows visually deselect.
export function ConversationDeriveControls({
  conversationId,
  selection,
}: Props): React.ReactElement {
  const [open, setOpen] = useState<DeriveKind | null>(null);
  return (
    <>
      <DeriveBar
        count={selection.count}
        onOpenIssue={() => setOpen('issue')}
        onOpenTask={() => setOpen('task')}
        onCancel={selection.exitSelectMode}
      />
      {open && (
        <DeriveModal
          kind={open}
          open
          sourceConversationId={conversationId}
          sourceMessageIds={Array.from(selection.selected)}
          onClose={() => setOpen(null)}
          onCreated={() => {
            // Clearing leaves select mode on so user can derive again
            // from the same surface — exit explicitly via Cancel.
            selection.clear();
          }}
        />
      )}
    </>
  );
}
