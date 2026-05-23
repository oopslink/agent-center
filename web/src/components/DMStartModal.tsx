import type React from 'react';
import { useState } from 'react';
import { useCreateConversation } from '@/api/conversations';
import { useAgents } from '@/api/agents';

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated?: (conversationId: string) => void;
}

// DMStartModal — pick 1+ peers + optional label → POST /api/conversations
// kind=dm. The peer list accepts free-form identity refs (one per line)
// so users can include identities (user:, agent:, system:*) that the
// agents endpoint doesn't surface. Live agents are offered as quick-pick
// chips to speed up the common case.
export function DMStartModal({
  open,
  onClose,
  onCreated,
}: Props): React.ReactElement | null {
  const [name, setName] = useState('');
  const [peers, setPeers] = useState('');
  const create = useCreateConversation();
  const agents = useAgents();
  if (!open) return null;

  const parsePeers = (raw: string): string[] =>
    raw
      .split(/[\n,]/)
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const members = parsePeers(peers);
    if (members.length === 0) return;
    try {
      const res = await create.mutateAsync({
        kind: 'dm',
        name: name.trim() || undefined,
        members,
      });
      onCreated?.(res.conversation_id);
      setName('');
      setPeers('');
      onClose();
    } catch {
      // error renders below; keep modal open for retry
    }
  };

  const addPeer = (id: string) => {
    setPeers((prev) => {
      if (parsePeers(prev).includes(id)) return prev;
      return prev.trim() ? `${prev}\n${id}` : id;
    });
  };

  return (
    <div
      className="fixed inset-0 z-10 flex items-center justify-center bg-slate-900/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="start-dm-title"
      data-testid="dm-start-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-white p-6 shadow-lg">
        <h2 id="start-dm-title" className="text-lg font-semibold">
          Start a DM
        </h2>
        <form className="mt-4 space-y-3" onSubmit={submit}>
          <div>
            <label className="block text-xs font-medium text-slate-700">
              Peer identity refs (one per line)
            </label>
            <textarea
              rows={3}
              value={peers}
              onChange={(e) => setPeers(e.target.value)}
              placeholder="agent:supervisor-1&#10;user:alice"
              autoFocus
              className="mt-1 w-full resize-none rounded border border-slate-300 px-2 py-1 font-mono text-xs focus:border-slate-500 focus:outline-none"
              data-testid="dm-peers-input"
            />
          </div>
          {agents.isSuccess && agents.data.length > 0 && (
            <div>
              <label className="block text-xs font-medium text-slate-700">
                Add an agent
              </label>
              <div
                className="mt-1 flex flex-wrap gap-1"
                data-testid="dm-agent-chips"
              >
                {agents.data.map((a) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => addPeer(a.identity_id)}
                    className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-700 hover:bg-slate-200"
                    data-testid="dm-agent-chip"
                    data-identity={a.identity_id}
                  >
                    {a.name}
                  </button>
                ))}
              </div>
            </div>
          )}
          <div>
            <label className="block text-xs font-medium text-slate-700">
              Label (optional)
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="leave blank for default"
              className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-slate-500 focus:outline-none"
              data-testid="dm-label-input"
            />
          </div>
          {create.isError && (
            <p className="text-xs text-red-600" data-testid="dm-start-error">
              {(create.error as Error).message}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-100"
              data-testid="dm-start-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={parsePeers(peers).length === 0 || create.isPending}
              className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-300"
              data-testid="dm-start-submit"
            >
              {create.isPending ? 'Starting…' : 'Start DM'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
