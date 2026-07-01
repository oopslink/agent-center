import type React from 'react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
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
    <div className="rounded-lg border border-border-base bg-bg-elevated p-6 text-sm text-text-muted" data-testid={testId}>
      {children}
    </div>
  );
}

export function AgentAnalyticsPanel({ agentId }: { agentId: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const now = useMemo(() => new Date(), []);
  const series = useAgentAnalytics(agentId);
  const monthRange = useMemo(() => ({ from: utcDay(now, -29), to: utcDay(now, 0), top: 5 }), [now]);
  const monthQ = useAgentAnalytics(agentId, monthRange);

  if (series.isLoading) {
    return <PanelMessage testId="agent-analytics-loading">{t('analytics.panel.loading')}</PanelMessage>;
  }
  if (series.isError || !series.data) {
    return <PanelMessage testId="agent-analytics-error">{t('analytics.panel.error')}</PanelMessage>;
  }

  const cards = deriveCards(series.data.heatmap, now);

  return (
    <div className="flex flex-col gap-5 lg:gap-6" data-testid="agent-analytics">
      <OverviewCards cards={cards} />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)] lg:items-stretch lg:gap-6">
        <AgentHeatmap cells={series.data.heatmap} today={now} />
        {monthQ.isLoading ? (
          <PanelMessage testId="agent-analytics-toptasks-loading">{t('analytics.panel.loadingTopTasks')}</PanelMessage>
        ) : (
          <TopCostTasks tasks={monthQ.data?.top_tasks ?? []} agentId={agentId} />
        )}
      </div>
      <TokensCostTrend byModel={series.data.trends.by_model} byProject={series.data.trends.by_project} />
    </div>
  );
}
