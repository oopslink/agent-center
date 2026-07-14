// Team WebUI — small shared building blocks (modal shell, inputs, cards, notes)
// on the console's semantic tokens. Keeps the feature's modals/panes visually
// consistent with the v7 mockup without a heavyweight component library.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useModalA11y } from '@/components/useModalA11y';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';
import { CloseIcon, InfoIcon } from './teamsUi';

// ---------------------------------------------------------------------------
// Tabs — WAI-ARIA tablist (manual activation) for the detail-page tab bars.
// ---------------------------------------------------------------------------

export interface TabDef<K extends string> {
  key: K;
  label: string;
}

export function Tabs<K extends string>({
  tabs,
  active,
  onChange,
  testId,
}: {
  tabs: ReadonlyArray<TabDef<K>>;
  active: K;
  onChange: (key: K) => void;
  testId?: string;
}): React.ReactElement {
  const tl = useTablistKeyboard({ keys: tabs.map((t) => t.key), active });
  return (
    <nav
      role="tablist"
      aria-orientation="horizontal"
      ref={tl.tablistRef as React.RefObject<HTMLElement>}
      onKeyDown={tl.onKeyDown}
      onBlur={tl.onBlur}
      data-testid={testId}
      className="mb-5 flex gap-1 border-b border-border-base"
    >
      {tabs.map((t) => {
        const on = t.key === active;
        return (
          <button
            key={t.key}
            role="tab"
            type="button"
            aria-selected={on}
            tabIndex={tl.tabIndexFor(t.key)}
            id={`tab-${t.key}`}
            aria-controls={`panel-${t.key}`}
            data-testid={`tab-${t.key}`}
            onClick={() => onChange(t.key)}
            className={[
              '-mb-px border-b-2 px-4 py-2.5 text-sm font-semibold motion-safe:transition-colors',
              on ? 'border-accent text-brand-hover' : 'border-transparent text-text-muted hover:text-text-primary',
            ].join(' ')}
          >
            {t.label}
          </button>
        );
      })}
    </nav>
  );
}

export const inputCls =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40';

export const btnPrimary =
  'inline-flex items-center gap-1.5 rounded bg-brand px-3.5 py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted';
export const btnGhost =
  'inline-flex items-center gap-1.5 rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm font-semibold text-text-secondary hover:bg-bg-subtle hover:text-text-primary';
export const btnSm =
  'inline-flex items-center gap-1.5 rounded border border-border-base bg-bg-elevated px-2.5 py-1 text-xs font-semibold text-text-secondary hover:bg-bg-subtle hover:text-text-primary';
export const btnSmPrimary =
  'inline-flex items-center gap-1.5 rounded bg-brand px-2.5 py-1 text-xs font-semibold text-white hover:bg-brand-hover';
export const btnSmDanger =
  'inline-flex items-center gap-1.5 rounded border border-danger px-2.5 py-1 text-xs font-semibold text-danger hover:bg-danger/10';

export function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: React.ReactNode;
  required?: boolean;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-4">
      <label className="mb-1.5 block text-xs font-semibold text-text-secondary">
        {label}
        {required && <span className="ml-1 text-accent">*</span>}
      </label>
      {children}
      {hint && <p className="mt-1.5 text-[0.6875rem] text-text-muted">{hint}</p>}
    </div>
  );
}

export function SmallLabel({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <span className="mb-1 block text-[0.65rem] font-semibold uppercase tracking-wider text-text-muted">
      {children}
    </span>
  );
}

export function Note({ children, testId }: { children: React.ReactNode; testId?: string }): React.ReactElement {
  return (
    <div
      data-testid={testId}
      className="mb-4 flex gap-2.5 rounded-lg border border-brand/25 bg-brand/5 px-3.5 py-3 text-xs text-text-secondary"
    >
      <span className="mt-0.5 text-brand">
        <InfoIcon className="h-4 w-4" />
      </span>
      <div className="[&_b]:font-semibold [&_b]:text-brand">{children}</div>
    </div>
  );
}

export function Card({
  children,
  className,
  testId,
}: {
  children: React.ReactNode;
  className?: string;
  testId?: string;
}): React.ReactElement {
  return (
    <div
      data-testid={testId}
      className={['rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1', className].filter(Boolean).join(' ')}
    >
      {children}
    </div>
  );
}

export function SectionHead({
  title,
  hint,
  action,
}: {
  title: string;
  hint?: React.ReactNode;
  action?: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3.5 flex items-center justify-between gap-3">
      <h3 className="text-[0.95rem] font-semibold text-text-primary">{title}</h3>
      {hint && <span className="ml-auto text-xs text-text-muted">{hint}</span>}
      {action}
    </div>
  );
}

export function SpecLine({ k, v }: { k: React.ReactNode; v: React.ReactNode }): React.ReactElement {
  return (
    <div className="flex items-center justify-between border-b border-border-base py-2 text-xs last:border-0">
      <span className="text-text-muted">{k}</span>
      <span className="font-mono text-text-primary">{v}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Modal shell — focus-trapped overlay with header / body / footer slots.
// ---------------------------------------------------------------------------

export function ModalShell({
  open,
  onClose,
  title,
  subtitle,
  wide,
  footer,
  children,
  testId,
}: {
  open: boolean;
  onClose: () => void;
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  wide?: boolean;
  footer?: React.ReactNode;
  children: React.ReactNode;
  testId?: string;
}): React.ReactElement | null {
  const { t } = useTranslation('teams');
  const containerRef = useModalA11y({ open, onClose });
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-auto bg-black/40 p-8 backdrop-blur-sm"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        data-testid={testId}
        className={[
          'w-full rounded-xl border border-border-strong bg-bg-elevated shadow-3',
          wide ? 'max-w-3xl' : 'max-w-xl',
        ].join(' ')}
      >
        <div className="flex items-start justify-between border-b border-border-base px-6 py-5">
          <div>
            <h2 className="text-base font-semibold text-text-primary">{title}</h2>
            {subtitle && <p className="mt-1 text-xs text-text-muted [&_b]:font-semibold [&_b]:text-text-primary">{subtitle}</p>}
          </div>
          <button
            type="button"
            className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary"
            onClick={onClose}
            aria-label={t('common.close')}
            data-testid={testId ? `${testId}-close` : undefined}
          >
            <CloseIcon className="h-5 w-5" />
          </button>
        </div>
        <div className="max-h-[62vh] overflow-auto px-6 py-5">{children}</div>
        {footer && (
          <div className="flex items-center justify-between gap-3 rounded-b-xl border-t border-border-base bg-bg-subtle px-6 py-4">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
