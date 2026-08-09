import type React from 'react';
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  useAIRuntimeCatalog,
  type AIRuntimeCatalog,
  type RuntimeCLI,
  type RuntimeModel,
} from '@/api/aiRuntime';
import { currentOrgSlug } from '@/api/client';

export type RuntimeModelValueMode = 'catalog-key' | 'model-key';

export interface RuntimeCatalogSelectorState {
  catalog?: AIRuntimeCatalog;
  isLoading?: boolean;
  error?: unknown;
  onRefresh?: () => void;
}

interface RuntimeCLISelectorProps extends RuntimeCatalogSelectorState {
  value: string;
  onChange: (value: string) => void;
  testId: string;
  ariaLabel: string;
  disabled?: boolean;
  includeUnknownValue?: boolean;
  className?: string;
}

interface RuntimeModelComboboxProps extends RuntimeCatalogSelectorState {
  value: string;
  onChange: (value: string) => void;
  testId: string;
  ariaLabel: string;
  cliKey?: string;
  disabled?: boolean;
  includeUnknownValue?: boolean;
  valueMode?: RuntimeModelValueMode;
  placeholder?: string;
  className?: string;
  modelsHref?: string;
}

interface ModelChoice {
  value: string;
  model?: RuntimeModel;
  missing?: boolean;
  unavailableReason?: string;
}

const baseInputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-60';

export function useRuntimeSelectorCatalog(): RuntimeCatalogSelectorState {
  const query = useAIRuntimeCatalog();
  const refresh = useCallback(() => {
    void query.refetch();
  }, [query]);
  return {
    catalog: query.data,
    isLoading: query.isLoading,
    error: query.error,
    onRefresh: refresh,
  };
}

export function aiRuntimeModelsPath(): string {
  const slug = currentOrgSlug();
  return slug ? `/organizations/${slug}/ai-runtime?tab=models` : '/ai-runtime?tab=models';
}

export function runtimeModelValue(model: RuntimeModel, mode: RuntimeModelValueMode = 'catalog-key'): string {
  return mode === 'model-key' ? (model.model_key || model.key) : model.key;
}

export function runtimeModelAllowsCLI(model: RuntimeModel, cliKey: string | undefined): boolean {
  if (!cliKey) return true;
  const compatible = model.compatible_cli_keys ?? [];
  // Missing compatibility metadata should not make the selector unusable; the
  // option row still shows the missing metadata instead of hiding the model.
  if (compatible.length === 0) return true;
  return compatible.includes(cliKey);
}

export function firstRuntimeCLIKey(catalog: AIRuntimeCatalog | undefined, preferred?: string): string {
  const enabled = selectableRuntimeCLIs(catalog);
  if (preferred && enabled.some((cli) => cli.key === preferred)) return preferred;
  return enabled[0]?.key ?? preferred ?? '';
}

export function firstRuntimeModelValue(
  catalog: AIRuntimeCatalog | undefined,
  cliKey: string | undefined,
  preferred?: string,
  mode: RuntimeModelValueMode = 'catalog-key',
): string {
  const choices = selectableRuntimeModelChoices(catalog, cliKey, '', false, mode);
  if (preferred && choices.some((choice) => choice.value === preferred)) return preferred;
  return choices[0]?.value ?? preferred ?? '';
}

export function isSelectableRuntimeModelValue(
  catalog: AIRuntimeCatalog | undefined,
  cliKey: string | undefined,
  value: string,
  mode: RuntimeModelValueMode = 'catalog-key',
): boolean {
  if (!value) return false;
  return selectableRuntimeModelChoices(catalog, cliKey, '', false, mode).some((choice) => choice.value === value);
}

export function RuntimeCLISelector({
  catalog,
  isLoading,
  error,
  onRefresh,
  value,
  onChange,
  testId,
  ariaLabel,
  disabled,
  includeUnknownValue = true,
  className,
}: RuntimeCLISelectorProps): React.ReactElement {
  const statusId = `${testId}-status`;
  const clis = useMemo(() => {
    const enabled = selectableRuntimeCLIs(catalog);
    if (!includeUnknownValue || !value || enabled.some((cli) => cli.key === value)) return enabled;
    const known = catalog?.clis.find((cli) => cli.key === value);
    if (known) return [known, ...enabled.filter((cli) => cli.key !== known.key)];
    return [
      {
        id: `missing-${value}`,
        key: value,
        display_name: value,
        executable: value,
        enabled: true,
      },
      ...enabled,
    ];
  }, [catalog, includeUnknownValue, value]);

  const empty = !isLoading && clis.length === 0;
  const selectDisabled = disabled || isLoading || empty;

  return (
    <div>
      <select
        className={[baseInputClass, className].filter(Boolean).join(' ')}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={selectDisabled}
        aria-label={ariaLabel}
        aria-describedby={statusId}
        data-testid={testId}
      >
        {clis.map((cli) => (
          <option key={cli.key} value={cli.key}>
            {runtimeCLILabel(cli)}
          </option>
        ))}
      </select>
      <SelectorStatus
        id={statusId}
        testId={testId}
        isLoading={isLoading}
        error={error}
        empty={empty}
        emptyLabel="No runtime CLIs available."
        onRefresh={onRefresh}
      />
    </div>
  );
}

export function RuntimeModelCombobox({
  catalog,
  isLoading,
  error,
  onRefresh,
  value,
  onChange,
  testId,
  ariaLabel,
  cliKey,
  disabled,
  includeUnknownValue = true,
  valueMode = 'catalog-key',
  placeholder = 'Filter models...',
  className,
  modelsHref,
}: RuntimeModelComboboxProps): React.ReactElement {
  useRefreshOnReturn(onRefresh);
  const inputId = useId();
  const listId = `${inputId}-listbox`;
  const statusId = `${testId}-status`;
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const [pos, setPos] = useState<{
    left: number;
    minWidth: number;
    maxHeight: number;
    top?: number;
    bottom?: number;
  } | null>(null);

  const selectedModel = useMemo(
    () => catalog?.models.find((model) => runtimeModelValue(model, valueMode) === value),
    [catalog, value, valueMode],
  );
  const selectedLabel = selectedModel ? runtimeModelDisplayName(selectedModel) : value;
  const selectedKnown = !!selectedModel;
  const choices = useMemo(
    () => selectableRuntimeModelChoices(catalog, cliKey, value, includeUnknownValue, valueMode),
    [catalog, cliKey, includeUnknownValue, value, valueMode],
  );
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return choices;
    return choices.filter((choice) => modelChoiceSearchText(choice).includes(needle));
  }, [choices, query]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query, choices.length]);

  const computePos = useCallback(() => {
    const el = inputRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const margin = 4;
    const spaceBelow = window.innerHeight - r.bottom - 8;
    const spaceAbove = r.top - 8;
    const below = spaceBelow >= 260 || spaceBelow >= spaceAbove;
    setPos({
      left: r.left,
      minWidth: Math.max(r.width, 320),
      maxHeight: Math.max(180, (below ? spaceBelow : spaceAbove) - margin),
      ...(below ? { top: r.bottom + margin } : { bottom: window.innerHeight - r.top + margin }),
    });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    computePos();
    const onMove = () => computePos();
    window.addEventListener('scroll', onMove, true);
    window.addEventListener('resize', onMove);
    return () => {
      window.removeEventListener('scroll', onMove, true);
      window.removeEventListener('resize', onMove);
    };
  }, [computePos, open]);

  const close = useCallback(() => {
    setOpen(false);
    setQuery('');
  }, []);

  useEffect(() => {
    if (!open) return;
    const handler = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!rootRef.current?.contains(target) && !popoverRef.current?.contains(target)) {
        close();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [close, open]);

  const choose = (choice: ModelChoice) => {
    if (!choice.missing && choice.value) {
      onChange(choice.value);
    }
    close();
    inputRef.current?.focus();
  };

  const activeChoice = filtered[activeIndex];
  const activeId = open && activeChoice ? `${listId}-option-${activeIndex}` : undefined;
  const empty = !isLoading && choices.length === 0;
  const noMatches = !isLoading && choices.length > 0 && filtered.length === 0;
  const inputDisabled = disabled || isLoading || empty;

  return (
    <div className="space-y-1" ref={rootRef}>
      <input
        ref={inputRef}
        className={[baseInputClass, className].filter(Boolean).join(' ')}
        value={open ? query : selectedLabel}
        onFocus={() => {
          if (!inputDisabled) {
            setOpen(true);
            setQuery('');
          }
        }}
        onClick={() => {
          if (!inputDisabled) {
            setOpen(true);
            setQuery('');
          }
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          if (!inputDisabled) setOpen(true);
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault();
            if (!open) {
              setOpen(true);
              setQuery('');
              return;
            }
            setActiveIndex((index) => Math.min(index + 1, Math.max(filtered.length - 1, 0)));
          } else if (event.key === 'ArrowUp') {
            event.preventDefault();
            if (!open) {
              setOpen(true);
              setQuery('');
              return;
            }
            setActiveIndex((index) => Math.max(index - 1, 0));
          } else if (event.key === 'Home' && open) {
            event.preventDefault();
            setActiveIndex(0);
          } else if (event.key === 'End' && open) {
            event.preventDefault();
            setActiveIndex(Math.max(filtered.length - 1, 0));
          } else if (event.key === 'Enter' && open) {
            event.preventDefault();
            if (activeChoice) choose(activeChoice);
          } else if (event.key === 'Escape') {
            event.preventDefault();
            close();
          } else if (event.key === 'Tab') {
            close();
          }
        }}
        disabled={inputDisabled}
        placeholder={placeholder}
        role="combobox"
        aria-label={ariaLabel}
        aria-autocomplete="list"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={activeId}
        aria-describedby={statusId}
        data-testid={testId}
      />
      <SelectorStatus
        id={statusId}
        testId={testId}
        isLoading={isLoading}
        error={error}
        empty={empty}
        emptyLabel={cliKey ? 'No runtime models are compatible with this CLI.' : 'No runtime models available.'}
        onRefresh={onRefresh}
      />
      {!selectedKnown && value && includeUnknownValue && (
        <p className="text-[0.6875rem] text-status-amber-fg" data-testid={`${testId}-missing`}>
          Model metadata unavailable for saved value "{value}".
        </p>
      )}
      {open && createPortal(
        <div
          ref={popoverRef}
          className="fixed z-50 flex flex-col overflow-hidden rounded border border-border-base bg-bg-elevated shadow-lg"
          style={{
            left: pos?.left,
            top: pos?.top,
            bottom: pos?.bottom,
            minWidth: pos?.minWidth,
            maxWidth: 'min(36rem, calc(100vw - 16px))',
            maxHeight: pos?.maxHeight,
            visibility: pos ? 'visible' : 'hidden',
          }}
          data-testid={`${testId}-popover`}
        >
          <ul id={listId} role="listbox" className="min-h-0 flex-1 overflow-y-auto py-1" data-testid={`${testId}-options`}>
            {isLoading && (
              <li className="px-3 py-2 text-xs text-text-muted" data-testid={`${testId}-loading`}>
                Loading runtime models...
              </li>
            )}
            {error && (
              <li className="px-3 py-2 text-xs text-danger" role="alert" data-testid={`${testId}-error`}>
                Runtime model catalog failed to load.
              </li>
            )}
            {empty && (
              <li className="px-3 py-2 text-xs text-text-muted" data-testid={`${testId}-empty`}>
                {cliKey ? 'No runtime models are compatible with this CLI.' : 'No runtime models available.'}
              </li>
            )}
            {noMatches && (
              <li className="px-3 py-2 text-xs text-text-muted" data-testid={`${testId}-empty`}>
                No matching runtime models.
              </li>
            )}
            {filtered.map((choice, index) => (
              <li
                key={`${choice.value}-${index}`}
                id={`${listId}-option-${index}`}
                role="option"
                aria-selected={choice.value === value}
                data-testid={`${testId}-option`}
                data-value={choice.value}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(choice)}
                className={[
                  'cursor-pointer px-3 py-2 text-left text-sm hover:bg-bg-subtle',
                  index === activeIndex ? 'bg-bg-subtle' : '',
                  choice.value === value ? 'text-brand' : 'text-text-primary',
                ].join(' ')}
              >
                <ModelChoiceRow choice={choice} selected={choice.value === value} />
              </li>
            ))}
          </ul>
          <div className="flex items-center justify-between gap-3 border-t border-border-base px-3 py-2 text-xs">
            <a
              href={modelsHref ?? aiRuntimeModelsPath()}
              target="_blank"
              rel="noreferrer"
              className="font-medium text-accent hover:underline"
              data-testid={`${testId}-models-link`}
            >
              AI Runtime Models
            </a>
            {onRefresh && (
              <button
                type="button"
                className="rounded border border-border-base px-2 py-1 text-text-secondary hover:bg-bg-subtle"
                onMouseDown={(event) => event.preventDefault()}
                onClick={onRefresh}
                data-testid={`${testId}-refresh`}
              >
                Refresh
              </button>
            )}
          </div>
        </div>,
        document.body,
      )}
    </div>
  );
}

function selectableRuntimeCLIs(catalog: AIRuntimeCatalog | undefined): RuntimeCLI[] {
  return (catalog?.clis ?? []).filter((cli) => cli.enabled);
}

function selectableRuntimeModelChoices(
  catalog: AIRuntimeCatalog | undefined,
  cliKey: string | undefined,
  selectedValue: string,
  includeUnknownValue: boolean,
  mode: RuntimeModelValueMode,
): ModelChoice[] {
  const models = catalog?.models ?? [];
  const choices: ModelChoice[] = models
    .filter((model) => model.enabled && runtimeModelAllowsCLI(model, cliKey))
    .map((model) => ({ value: runtimeModelValue(model, mode), model }));
  const knownSelected = selectedValue
    ? models.find((model) => runtimeModelValue(model, mode) === selectedValue)
    : undefined;
  if (knownSelected && includeUnknownValue && !choices.some((choice) => choice.value === selectedValue)) {
    choices.unshift({
      value: runtimeModelValue(knownSelected, mode),
      model: knownSelected,
      unavailableReason: knownSelected.enabled
        ? 'Not compatible with selected CLI'
        : 'Disabled in AI Runtime',
    });
  } else if (!knownSelected && includeUnknownValue && selectedValue) {
    choices.unshift({ value: selectedValue, missing: true });
  }
  return choices;
}

function runtimeCLILabel(cli: RuntimeCLI): string {
  return cli.display_name && cli.display_name !== cli.key ? `${cli.display_name} (${cli.key})` : cli.key;
}

function runtimeModelDisplayName(model: RuntimeModel): string {
  return model.display_name || model.model_key || model.key;
}

function modelChoiceSearchText(choice: ModelChoice): string {
  if (choice.missing || !choice.model) return choice.value.toLowerCase();
  const model = choice.model;
  return [
    choice.value,
    model.key,
    model.model_key,
    model.display_name,
    model.tier,
    formatContext(model.context_window),
    formatCost(model.input_cost_per_mtok, model.output_cost_per_mtok),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
}

function ModelChoiceRow({ choice, selected }: { choice: ModelChoice; selected: boolean }): React.ReactElement {
  if (choice.missing || !choice.model) {
    return (
      <span className="block">
        <span className="flex items-center justify-between gap-2">
          <span className="font-mono text-sm">{choice.value}</span>
          {selected && <span className="shrink-0 text-[0.6875rem] font-medium uppercase">Selected</span>}
        </span>
        <span className="mt-1 block text-xs text-status-amber-fg">Metadata missing from AI Runtime Models</span>
      </span>
    );
  }
  const model = choice.model;
  return (
    <span className="block min-w-0">
      <span className="flex items-start justify-between gap-2">
        <span className="min-w-0">
          <span className="block truncate font-medium">{runtimeModelDisplayName(model)}</span>
          <span className="block truncate font-mono text-xs text-text-muted">{choice.value}</span>
        </span>
        {selected && <span className="shrink-0 text-[0.6875rem] font-medium uppercase">Selected</span>}
      </span>
      <span className="mt-1 flex flex-wrap gap-1.5">
        <MetaPill>{model.tier || 'Tier unavailable'}</MetaPill>
        <MetaPill>{formatContext(model.context_window)}</MetaPill>
        <MetaPill>{formatCost(model.input_cost_per_mtok, model.output_cost_per_mtok)}</MetaPill>
        {choice.unavailableReason && <MetaPill tone="warning">{choice.unavailableReason}</MetaPill>}
      </span>
    </span>
  );
}

function MetaPill({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: 'neutral' | 'warning' }): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-1.5 py-0.5 text-[0.6875rem]',
        tone === 'warning'
          ? 'bg-status-amber-bg text-status-amber-fg'
          : 'bg-bg-subtle text-text-secondary',
      ].join(' ')}
    >
      {children}
    </span>
  );
}

function formatContext(value: number | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? `${value.toLocaleString()} ctx`
    : 'Context unavailable';
}

function formatCost(input: number | undefined, output: number | undefined): string {
  if (input === undefined && output === undefined) return 'Cost unavailable';
  const inLabel = input === undefined ? '?' : `$${input}`;
  const outLabel = output === undefined ? '?' : `$${output}`;
  return `in ${inLabel} / out ${outLabel} per MTok`;
}

function SelectorStatus({
  id,
  testId,
  isLoading,
  error,
  empty,
  emptyLabel,
  onRefresh,
}: {
  id: string;
  testId: string;
  isLoading?: boolean;
  error?: unknown;
  empty: boolean;
  emptyLabel: string;
  onRefresh?: () => void;
}): React.ReactElement | null {
  if (isLoading) {
    return <p id={id} className="mt-1 text-[0.6875rem] text-text-muted" data-testid={`${testId}-loading`}>Loading runtime catalog...</p>;
  }
  if (error) {
    return (
      <p id={id} className="mt-1 text-[0.6875rem] text-danger" data-testid={`${testId}-error`}>
        Runtime catalog unavailable.
        {onRefresh && (
          <button type="button" className="ml-2 text-accent hover:underline" onClick={onRefresh} data-testid={`${testId}-refresh`}>
            Refresh
          </button>
        )}
      </p>
    );
  }
  if (empty) {
    return <p id={id} className="mt-1 text-[0.6875rem] text-text-muted" data-testid={`${testId}-empty`}>{emptyLabel}</p>;
  }
  return null;
}

function useRefreshOnReturn(onRefresh: (() => void) | undefined): void {
  useEffect(() => {
    if (!onRefresh) return undefined;
    const refresh = () => {
      if (document.visibilityState === 'visible') onRefresh();
    };
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      window.removeEventListener('focus', refresh);
      document.removeEventListener('visibilitychange', refresh);
    };
  }, [onRefresh]);
}
