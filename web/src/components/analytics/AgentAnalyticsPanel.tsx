import type React from 'react';
import { useMemo } from 'react';
import { useAgentAnalytics } from '@/api/analytics';
import { deriveCards, utcDay } from '@/utils/analyticsWindows';
import { OverviewCards } from './OverviewCards';
import { TokensCostTrend } from './TokensCostTrend';
import { TopCostTasks } from './TopCostTasks';

// I28/F6 panel: composes the three F6 blocks — overview cards, Tokens & Cost
// trend, and Top Cost Tasks — for one agent. It owns two reads:
//   · the standard ~53-week analytics series (cards + the multi-month trend), and
//   · a THIS-MONTH-scoped query whose top_tasks is exactly the current month
//     (Top Cost Tasks must not show a wider window under its label).
// The cards derive entirely from the per-day heatmap series (deriveCards).
//
// The F5 activity heatmap is NOT rendered here — it is dev1's component; the F7
// agent-page tab places it between these cards and the trend row. F7 can reuse
// useAgentAnalytics so the heatmap shares this panel's series fetch.

function PanelMessage({ children, testId }: { children: React.ReactNode; testId: string }): React.ReactElement {
  return (
    <div className="rounded-lg border border-border bg-bg-elevated p-6 text-sm text-text-muted" data-testid={testId}>
      {children}
    </div>
  );
}

export function AgentAnalyticsPanel({ agentId }: { agentId: string }): React.ReactElement {
  const now = useMemo(() => new Date(), []);
  const series = useAgentAnalytics(agentId);
  const monthRange = useMemo(() => ({ from: utcDay(now, -29), to: utcDay(now, 0), top: 5 }), [now]);
  const monthQ = useAgentAnalytics(agentId, monthRange);

  if (series.isLoading) {
    return <PanelMessage testId="agent-analytics-loading">Loading analytics…</PanelMessage>;
  }
  if (series.isError || !series.data) {
    return <PanelMessage testId="agent-analytics-error">Failed to load analytics.</PanelMessage>;
  }

  const cards = deriveCards(series.data.heatmap, now);

  return (
    <div className="flex flex-col gap-4" data-testid="agent-analytics">
      <OverviewCards cards={cards} />
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <TokensCostTrend byModel={series.data.trends.by_model} byProject={series.data.trends.by_project} />
        </div>
        <div>
          {monthQ.isLoading ? (
            <PanelMessage testId="agent-analytics-toptasks-loading">Loading top tasks…</PanelMessage>
          ) : (
            <TopCostTasks tasks={monthQ.data?.top_tasks ?? []} agentId={agentId} />
          )}
        </div>
      </div>
    </div>
  );
}
