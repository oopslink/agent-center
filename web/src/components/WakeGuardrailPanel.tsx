import type React from 'react';
import { useEffect, useState } from 'react';
import { useWakeGuardrail, useUpdateWakeGuardrail, type WakeGuardrail } from '@/api/system';

// I7-D3 — the wake-guardrail (唤醒护栏) params panel under System → Settings.
// Exposes D1's five live center settings (GET/PUT /api/system/wake-guardrail):
// view current effective values, edit, and save-to-take-effect (the WakeGuard
// reads the store on every evaluation, so a save applies immediately — no
// restart). All five thresholds must be > 0 (the §3.5 contract the BE enforces);
// we mirror that here for fast feedback + show each field's conservative default.

interface FieldSpec {
  key: keyof WakeGuardrail;
  label: string;
  hint: string;
  def: number;
}

// Defaults mirror wakeguard.DefaultConfig (depth 4, 5min/3, 10/min, budget 16).
const FIELDS: ReadonlyArray<FieldSpec> = [
  { key: 'max_depth', label: 'Max wake-chain depth', hint: 'Max hops in one wake chain; exceeding it trips the breaker', def: 4 },
  { key: 'cycle_window_sec', label: 'Cycle-detection window (seconds)', hint: 'Round-trips are counted within this rolling window', def: 300 },
  { key: 'cycle_threshold', label: 'Cycle threshold (round-trips per window)', hint: 'Reaching this many round-trips in the window trips the breaker', def: 3 },
  { key: 'rate_per_min', label: 'Rate limit (agent wakes per minute)', hint: 'Tokens for agent-driven wakes of a single agent per minute', def: 10 },
  { key: 'chain_token_budget', label: 'Chain token budget (root)', hint: 'Cost budget per wake chain, decremented each hop', def: 16 },
];

export function WakeGuardrailPanel(): React.ReactElement {
  const { data, isLoading, isError, refetch } = useWakeGuardrail();
  const update = useUpdateWakeGuardrail();
  const [form, setForm] = useState<WakeGuardrail | null>(null);
  const [saved, setSaved] = useState(false);

  // Seed the editable form once the effective config loads.
  useEffect(() => {
    if (data) setForm(data);
  }, [data]);

  // Auto-dismiss the save confirmation after a few seconds (toast-like).
  useEffect(() => {
    if (!saved) return;
    const t = setTimeout(() => setSaved(false), 3000);
    return () => clearTimeout(t);
  }, [saved]);

  // Error is checked BEFORE the loading gate: on a failed fetch the query
  // settles with isError=true but data/form stay null, so an `isLoading || !form`
  // gate placed first would mask the failure as a perpetual "加载护栏参数…"
  // (T249). Surface an explicit error with a retry instead.
  if (isError) {
    return (
      <div
        className="flex items-center justify-between gap-4 rounded-xl border border-danger/30 bg-danger/5 p-5"
        data-testid="wake-guardrail-error"
      >
        <p className="text-sm text-danger">Failed to load guardrail settings.</p>
        <button
          type="button"
          onClick={() => void refetch()}
          className="shrink-0 rounded-md border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
          data-testid="wake-guardrail-retry"
        >
          Retry
        </button>
      </div>
    );
  }
  if (isLoading || !form) {
    return (
      <p className="text-sm text-text-muted" data-testid="wake-guardrail-loading">
        Loading guardrail settings…
      </p>
    );
  }

  const invalid = FIELDS.some((f) => !Number.isFinite(form[f.key]) || form[f.key] <= 0);

  function setField(key: keyof WakeGuardrail, raw: string): void {
    setSaved(false);
    setForm((prev) => (prev ? { ...prev, [key]: raw === '' ? NaN : Number(raw) } : prev));
  }

  function save(): void {
    if (!form || invalid) return;
    setSaved(false);
    update.mutate(form, { onSuccess: (d) => { setForm(d); setSaved(true); } });
  }

  return (
    <div
      className="space-y-4 rounded-xl border border-border-base bg-bg-elevated p-5"
      data-testid="wake-guardrail-panel"
    >
      <div>
        <h2 className="text-base font-semibold text-text-primary">Wake guardrails</h2>
        <p className="mt-0.5 text-xs text-text-muted">
          Prevent runaway agent-to-agent wakes (chain depth / cycles / rate limit / token budget). Applied immediately on save — no restart needed.
        </p>
      </div>

      <div className="space-y-3">
        {FIELDS.map((f) => {
          const val = form[f.key];
          const bad = !Number.isFinite(val) || val <= 0;
          return (
            <label key={f.key} className="flex items-center justify-between gap-4">
              <span className="text-xs text-text-secondary">
                {f.label}
                <span className="block text-text-muted">
                  {f.hint} · default {f.def}
                </span>
              </span>
              <input
                type="number"
                min={1}
                step={1}
                value={Number.isFinite(val) ? String(val) : ''}
                onChange={(e) => setField(f.key, e.target.value)}
                aria-label={f.label}
                aria-invalid={bad}
                data-testid={`wake-guardrail-${f.key}`}
                className={`w-28 rounded-md border bg-bg-base px-3 py-1.5 text-right text-sm ${
                  bad ? 'border-danger' : 'border-border-base'
                }`}
              />
            </label>
          );
        })}
      </div>

      {invalid && (
        <p className="text-xs text-danger" data-testid="wake-guardrail-invalid">
          All thresholds must be positive integers (&gt; 0).
        </p>
      )}
      {update.isError && (
        <p className="text-xs text-danger" data-testid="wake-guardrail-save-error">
          Save failed: {update.error instanceof Error ? update.error.message : 'Unknown error'}
        </p>
      )}

      <div className="flex items-center gap-3">
        <button
          type="button"
          disabled={invalid || update.isPending}
          onClick={save}
          className="rounded-md bg-brand px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          data-testid="wake-guardrail-save"
        >
          {update.isPending ? 'Saving…' : 'Save'}
        </button>
        {saved && !update.isPending && (
          <span
            role="status"
            aria-live="polite"
            className="inline-flex items-center rounded-full bg-success/15 px-2.5 py-1 text-xs font-medium text-success"
            data-testid="wake-guardrail-saved"
          >
            Saved and applied
          </span>
        )}
      </div>
    </div>
  );
}
