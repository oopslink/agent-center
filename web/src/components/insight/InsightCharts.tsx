import type React from 'react';
import { Link } from 'react-router-dom';

export type ChartTone = 'success' | 'danger' | 'warning' | 'info' | 'neutral';

export interface ChartDatum {
  key: string;
  label: string;
  value: number;
  tone?: ChartTone;
  href?: string;
  detail?: string;
}

const EMPTY = '—';

export function ChartPanel({ title, subtitle, children, testId }: { title: string; subtitle?: string; children: React.ReactNode; testId?: string }): React.ReactElement {
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid={testId}>
      <div className="mb-3">
        <h2 className="text-sm font-semibold text-text-primary">{title}</h2>
        {subtitle && <p className="mt-1 text-xs text-text-muted">{subtitle}</p>}
      </div>
      {children}
    </section>
  );
}

export function DonutChart({ data, totalLabel }: { data: ChartDatum[]; totalLabel: string }): React.ReactElement {
  const total = data.reduce((sum, item) => sum + Math.max(0, item.value), 0);
  const radius = 38;
  const circumference = 2 * Math.PI * radius;
  let offset = 0;
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
      <svg viewBox="0 0 104 104" role="img" aria-label={totalLabel} className="h-28 w-28 flex-none">
        <circle cx="52" cy="52" r={radius} fill="none" stroke="currentColor" strokeWidth="14" className="text-bg-subtle" />
        {data.map((item) => {
          const length = total > 0 ? (Math.max(0, item.value) / total) * circumference : 0;
          const dash = `${length} ${circumference - length}`;
          const segment = (
            <circle
              key={item.key}
              cx="52"
              cy="52"
              r={radius}
              fill="none"
              stroke="currentColor"
              strokeWidth="14"
              strokeLinecap="butt"
              strokeDasharray={dash}
              strokeDashoffset={-offset}
              transform="rotate(-90 52 52)"
              className={toneTextClass(item.tone)}
            />
          );
          offset += length;
          return segment;
        })}
        <text x="52" y="48" textAnchor="middle" className="fill-text-primary text-[18px] font-semibold tabular-nums">{total}</text>
        <text x="52" y="63" textAnchor="middle" className="fill-text-muted text-[9px]">{totalLabel}</text>
      </svg>
      <div className="grid flex-1 gap-2">
        {data.map((item) => <ChartLegendRow key={item.key} item={item} total={total} />)}
      </div>
    </div>
  );
}

export function HorizontalBars({ data, emptyLabel, maxValue }: { data: ChartDatum[]; emptyLabel: string; maxValue?: number }): React.ReactElement {
  const max = Math.max(maxValue ?? 0, ...data.map((item) => Math.max(0, item.value)));
  if (data.length === 0 || max === 0) return <p className="text-sm text-text-muted">{emptyLabel}</p>;
  return (
    <div className="space-y-2">
      {data.map((item) => {
        const width = `${Math.max(4, Math.round((Math.max(0, item.value) / max) * 100))}%`;
        const label = <span className="truncate text-sm font-medium text-text-primary">{item.label}</span>;
        return (
          <div key={item.key} className="grid gap-1">
            <div className="flex min-w-0 items-baseline justify-between gap-3">
              {item.href ? <Link to={item.href} className="min-w-0 truncate text-brand hover:underline">{label}</Link> : label}
              <span className="shrink-0 text-sm tabular-nums text-text-secondary">{item.value}</span>
            </div>
            <div className="h-2 rounded bg-bg-subtle">
              <div className={`h-2 rounded ${toneBgClass(item.tone)}`} style={{ width }} />
            </div>
            {item.detail && <div className="truncate text-xs text-text-muted">{item.detail}</div>}
          </div>
        );
      })}
    </div>
  );
}

export function SegmentedBar({ data, emptyLabel }: { data: ChartDatum[]; emptyLabel: string }): React.ReactElement {
  const total = data.reduce((sum, item) => sum + Math.max(0, item.value), 0);
  if (total === 0) return <p className="text-sm text-text-muted">{emptyLabel}</p>;
  return (
    <div>
      <div className="flex h-3 overflow-hidden rounded bg-bg-subtle">
        {data.map((item) => (
          <div
            key={item.key}
            className={toneBgClass(item.tone)}
            style={{ width: `${Math.max(0, item.value) / total * 100}%` }}
            title={`${item.label}: ${item.value}`}
          />
        ))}
      </div>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        {data.map((item) => <ChartLegendRow key={item.key} item={item} total={total} />)}
      </div>
    </div>
  );
}

export function LineChart({ data, emptyLabel, valueLabel, formatValue = String }: { data: ChartDatum[]; emptyLabel: string; valueLabel: string; formatValue?: (value: number) => string }): React.ReactElement {
  const max = Math.max(0, ...data.map((item) => Math.max(0, item.value)));
  if (data.length === 0 || max === 0) return <p className="text-sm text-text-muted">{emptyLabel}</p>;
  const width = 320;
  const height = 104;
  const padX = 8;
  const padY = 10;
  const step = data.length > 1 ? (width - padX * 2) / (data.length - 1) : 0;
  const points = data.map((item, index) => {
    const x = padX + index * step;
    const y = height - padY - (Math.max(0, item.value) / max) * (height - padY * 2);
    return { item, x, y };
  });
  const d = points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x} ${point.y}`).join(' ');
  return (
    <div>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={valueLabel} className="h-32 w-full overflow-visible">
        <path d={`M ${padX} ${height - padY} H ${width - padX}`} fill="none" stroke="currentColor" strokeWidth="1" className="text-border-base" />
        <path d={d} fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="text-brand" />
        {points.map(({ item, x, y }) => (
          <circle key={item.key} cx={x} cy={y} r="3" className="fill-brand" />
        ))}
      </svg>
      <div className="mt-2 flex items-center justify-between gap-3 text-xs text-text-muted">
        <span className="truncate">{data[0]?.label ?? EMPTY}</span>
        <span className="shrink-0 tabular-nums">{valueLabel}: {formatValue(max)}</span>
        <span className="truncate text-right">{data[data.length - 1]?.label ?? EMPTY}</span>
      </div>
    </div>
  );
}

function ChartLegendRow({ item, total }: { item: ChartDatum; total: number }): React.ReactElement {
  const pct = total > 0 ? Math.round((Math.max(0, item.value) / total) * 1000) / 10 : 0;
  return (
    <div className="flex items-center justify-between gap-3 rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm">
      <div className="flex min-w-0 items-center gap-2">
        <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${toneBgClass(item.tone)}`} />
        <span className="truncate text-text-primary">{item.label}</span>
      </div>
      <span className="shrink-0 tabular-nums text-text-secondary">{item.value} · {total > 0 ? `${pct}%` : EMPTY}</span>
    </div>
  );
}

function toneBgClass(tone: ChartTone = 'neutral'): string {
  switch (tone) {
    case 'success':
      return 'bg-success';
    case 'danger':
      return 'bg-danger';
    case 'warning':
      return 'bg-warning';
    case 'info':
      return 'bg-brand';
    default:
      return 'bg-text-muted';
  }
}

function toneTextClass(tone: ChartTone = 'neutral'): string {
  switch (tone) {
    case 'success':
      return 'text-success';
    case 'danger':
      return 'text-danger';
    case 'warning':
      return 'text-warning';
    case 'info':
      return 'text-brand';
    default:
      return 'text-text-muted';
  }
}
