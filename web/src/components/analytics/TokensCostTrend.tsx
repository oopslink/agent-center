import type React from 'react';
import { useMemo, useState } from 'react';
import type { AnalyticsModelTrendPoint, AnalyticsProjectTrendPoint } from '@/api/types';
import { OrgLink } from '@/OrgContext';
import { useProjects } from '@/api/projects';
import { formatTokens, formatCostMicros } from '@/utils/format';
import { modelColor, modelShortLabel } from './modelColors';

// T472: the project dimension's legend key is the raw project_id; resolve it to
// the project NAME and link it to the project detail page (@oopslink). "" maps to
// the synthetic "(no project)" bucket (no link).
const NO_PROJECT = '(no project)';

// I28/F6 Tokens & Cost Trend (1:1 with the English mockup): a stacked-area trend
// over the full series span. Two toggles realize the Build Spec's "model & project
// 两维堆叠" + the "Tokens & Cost" title: metric (Tokens | Cost) × dimension
// (Model | Project). Hand-rolled SVG — the SPA has no charting dependency.
// Read-model split (PD): the model dimension comes from usage_events (by_model),
// the project dimension from the rollup (by_project).

const VIEW_W = 640;
const VIEW_H = 220;

// project series palette (data-viz, applied via inline fill — see modelColors.ts
// note). raw-color-ok: data-visualization series palette.
const PROJECT_PALETTE = ['#3b82f6', '#22c55e', '#8b5cf6', '#f59e0b', '#ec4899', '#14b8a6', '#64748b'];

type Metric = 'tokens' | 'cost';
type Dimension = 'model' | 'project';

interface SeriesInput {
  day: string;
  key: string; // model id or project_id
  value: number;
}

// flatten maps the active (metric, dimension) selection to a flat (day, key,
// value) list from the right source series.
function flatten(
  metric: Metric,
  dim: Dimension,
  byModel: AnalyticsModelTrendPoint[],
  byProject: AnalyticsProjectTrendPoint[],
): SeriesInput[] {
  if (dim === 'model') {
    return byModel.map((p) => ({
      day: p.day,
      key: p.model,
      value: metric === 'cost' ? p.cost_micros : p.tokens_in + p.tokens_out,
    }));
  }
  return byProject.map((p) => ({
    day: p.day,
    key: p.project_id === '' ? NO_PROJECT : p.project_id,
    value: metric === 'cost' ? p.cost_micros : p.tokens_in + p.tokens_out,
  }));
}

interface StackModel {
  days: string[];
  keys: string[];
  max: number;
  // areas[key] = SVG path string for that series' stacked band.
  areas: Record<string, string>;
}

function buildStack(input: SeriesInput[]): StackModel {
  const days = Array.from(new Set(input.map((p) => p.day))).sort();
  // series keys ordered by total value desc → larger bands sit at the bottom.
  const totals = new Map<string, number>();
  for (const p of input) totals.set(p.key, (totals.get(p.key) ?? 0) + p.value);
  const keys = Array.from(totals.keys()).sort((a, b) => (totals.get(b) ?? 0) - (totals.get(a) ?? 0));

  const valueAt = new Map<string, number>();
  for (const p of input) valueAt.set(`${p.day}|${p.key}`, (valueAt.get(`${p.day}|${p.key}`) ?? 0) + p.value);

  // cumulative top per (dayIndex) as we stack keys; track per-day running offset.
  const dayIndex = new Map(days.map((d, i) => [d, i]));
  const x = (d: string) => (days.length <= 1 ? 0 : ((dayIndex.get(d) ?? 0) / (days.length - 1)) * VIEW_W);

  // compute max stacked total for y-scale.
  let max = 0;
  for (const d of days) {
    let sum = 0;
    for (const k of keys) sum += valueAt.get(`${d}|${k}`) ?? 0;
    max = Math.max(max, sum);
  }
  const y = (v: number) => (max <= 0 ? VIEW_H : VIEW_H - (v / max) * VIEW_H);

  const lower = new Map(days.map((d) => [d, 0])); // running baseline per day
  const areas: Record<string, string> = {};
  for (const k of keys) {
    const tops: Array<[number, number]> = [];
    const bottoms: Array<[number, number]> = [];
    for (const d of days) {
      const base = lower.get(d) ?? 0;
      const top = base + (valueAt.get(`${d}|${k}`) ?? 0);
      tops.push([x(d), y(top)]);
      bottoms.push([x(d), y(base)]);
      lower.set(d, top);
    }
    // polygon: forward along tops, back along bottoms.
    const fwd = tops.map(([px, py]) => `${px.toFixed(1)},${py.toFixed(1)}`).join(' L');
    const back = bottoms
      .reverse()
      .map(([px, py]) => `${px.toFixed(1)},${py.toFixed(1)}`)
      .join(' L');
    areas[k] = `M${fwd} L${back} Z`;
  }
  return { days, keys, max, areas };
}

function seriesColor(dim: Dimension, key: string, index: number): string {
  return dim === 'model' ? modelColor(key) : PROJECT_PALETTE[index % PROJECT_PALETTE.length];
}

function seriesLabel(dim: Dimension, key: string): string {
  return dim === 'model' ? modelShortLabel(key) : key;
}

function Toggle<T extends string>({
  value,
  options,
  onChange,
  testId,
}: {
  value: T;
  options: ReadonlyArray<{ value: T; label: string }>;
  onChange: (v: T) => void;
  testId: string;
}): React.ReactElement {
  return (
    <div className="inline-flex overflow-hidden rounded-md border border-border text-xs" role="group" data-testid={testId}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          aria-pressed={value === o.value}
          className={[
            'px-2 py-1',
            value === o.value ? 'bg-brand text-white' : 'bg-bg-elevated text-text-muted hover:bg-bg-subtle',
          ].join(' ')}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function TokensCostTrend({
  byModel,
  byProject,
}: {
  byModel: AnalyticsModelTrendPoint[];
  byProject: AnalyticsProjectTrendPoint[];
}): React.ReactElement {
  const [metric, setMetric] = useState<Metric>('tokens');
  const [dim, setDim] = useState<Dimension>('model');
  // T472: resolve project_id → name for the project-dimension legend (links to
  // the project detail page). Fail-soft: an unresolved id falls back to itself.
  const projects = useProjects();
  const projectName = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of projects.data ?? []) m.set(p.id, p.name);
    return m;
  }, [projects.data]);

  const stack = useMemo(
    () => buildStack(flatten(metric, dim, byModel, byProject)),
    [metric, dim, byModel, byProject],
  );
  const fmtMax = metric === 'cost' ? formatCostMicros(stack.max) : formatTokens(stack.max);
  const hasData = stack.days.length > 0 && stack.max > 0;

  return (
    <section className="flex flex-col rounded-lg border border-border bg-bg-elevated p-4" data-testid="analytics-trend">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-text">Tokens &amp; Cost Trend</h3>
        <div className="flex items-center gap-2">
          <Toggle
            value={metric}
            onChange={setMetric}
            testId="trend-metric-toggle"
            options={[
              { value: 'tokens', label: 'Tokens' },
              { value: 'cost', label: 'Cost' },
            ]}
          />
          <Toggle
            value={dim}
            onChange={setDim}
            testId="trend-dim-toggle"
            options={[
              { value: 'model', label: 'Model' },
              { value: 'project', label: 'Project' },
            ]}
          />
        </div>
      </div>

      {!hasData ? (
        <p className="py-8 text-center text-xs text-text-muted" data-testid="analytics-trend-empty">
          No {metric} data to chart.
        </p>
      ) : (
        <>
          <div className="flex gap-2">
            <div className="flex flex-col justify-between py-1 text-[0.625rem] tabular-nums text-text-muted">
              <span>{fmtMax}</span>
              <span>0</span>
            </div>
            <svg
              viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
              preserveAspectRatio="none"
              className="h-44 w-full"
              role="img"
              aria-label={`${metric} trend stacked by ${dim}`}
              data-testid="analytics-trend-svg"
            >
              {stack.keys.map((k, i) => (
                <path
                  key={k}
                  d={stack.areas[k]}
                  style={{ fill: seriesColor(dim, k, i), fillOpacity: 0.55 }}
                  data-testid={`trend-area-${k}`}
                />
              ))}
            </svg>
          </div>
          <ul className="mt-2 flex flex-wrap gap-3" data-testid="analytics-trend-legend">
            {stack.keys.map((k, i) => (
              <li key={k} className="flex items-center gap-1.5 text-xs text-text-muted">
                <span className="h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: seriesColor(dim, k, i) }} />
                {dim === 'project' && k !== NO_PROJECT ? (
                  <OrgLink
                    to={`/projects/${encodeURIComponent(k)}`}
                    className="text-accent hover:underline"
                    data-testid="trend-legend-project-link"
                    data-project-id={k}
                    title={`Open project ${projectName.get(k) ?? k}`}
                  >
                    {projectName.get(k) ?? k}
                  </OrgLink>
                ) : (
                  seriesLabel(dim, k)
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
