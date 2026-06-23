import type React from 'react';
import { useMemo } from 'react';
import { useAgentAnalytics } from '@/api/analytics';
import { deriveCards, utcDay } from '@/utils/analyticsWindows';
import AgentHeatmap from '@/components/AgentHeatmap';
import { OverviewCards } from './OverviewCards';
import { TokensCostTrend } from './TokensCostTrend';
import { TopCostTasks } from './TopCostTasks';

// I28/F6+F7 panel: composes the full per-agent analytics surface — overview
// cards, the F5 activity heatmap, the Tokens & Cost trend, and Top Cost Tasks.
// It owns two reads:
//   · the standard ~53-week analytics series (cards + heatmap + the multi-month
//     trend), and
//   · a THIS-MONTH-scoped query whose top_tasks is exactly the current month
//     (Top Cost Tasks must not show a wider window under its label).
// The cards derive entirely from the per-day heatmap series (deriveCards).
//
// F7 wiring: the F5 heatmap renders between the cards and the trend row (mockup
// order) and shares this panel's series fetch — series.data.heatmap is exactly
// AgentHeatmap's `cells` prop, so no extra request is made. AgentDetail's
// `analytics` tab mounts this panel (route /agents/:id?tab=analytics).

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
      <AgentHeatmap cells={series.data.heatmap} today={now} />
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
