import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type React from 'react';
import {
  firstRuntimeModelValue,
  isSelectableRuntimeModelValue,
  isSelectableRuntimePair,
  RuntimeCLISelector,
  RuntimeModelCombobox,
  type RuntimeModelValueMode,
} from './RuntimeSelectors';
import type { AIRuntimeCatalog } from '@/api/aiRuntime';

const catalog: AIRuntimeCatalog = {
  revision: 7,
  clis: [
    {
      id: 'cli-claude',
      key: 'claude-code',
      display_name: 'Claude Code',
      executable: 'claude',
      enabled: true,
    },
    {
      id: 'cli-codex',
      key: 'codex',
      display_name: 'Codex CLI',
      executable: 'codex',
      enabled: true,
    },
  ],
  models: [
    {
      id: 'model-gpt-5',
      key: 'gpt-5',
      model_key: 'gpt-5',
      display_name: 'GPT-5',
      compatible_cli_keys: ['codex'],
      enabled: true,
      context_window: 400000,
      input_cost_per_mtok: 1.25,
      output_cost_per_mtok: 10,
      tier: 'frontier',
    },
    {
      id: 'model-sonnet',
      key: 'sonnet-5',
      model_key: 'sonnet-5',
      display_name: 'Sonnet 5',
      compatible_cli_keys: ['claude-code'],
      enabled: true,
      context_window: 200000,
      input_cost_per_mtok: 3,
      output_cost_per_mtok: 15,
      tier: 'standard',
    },
    {
      id: 'model-metadata-thin',
      key: 'metadata-thin',
      model_key: 'metadata-thin',
      display_name: 'Metadata Thin',
      compatible_cli_keys: ['claude-code'],
      enabled: true,
    },
  ],
};

function renderModel(
  props: Partial<React.ComponentProps<typeof RuntimeModelCombobox>> = {},
  mode: RuntimeModelValueMode = 'catalog-key',
) {
  const onChange = vi.fn();
  render(
    <RuntimeModelCombobox
      testId="model"
      ariaLabel="Model"
      catalog={catalog}
      value="gpt-5"
      onChange={onChange}
      valueMode={mode}
      {...props}
    />,
  );
  return { onChange };
}

describe('RuntimeModelCombobox', () => {
  afterEach(() => cleanup());

  it('falls back to a compatible model when the preferred value is not selectable', () => {
    expect(firstRuntimeModelValue(catalog, 'claude-code', 'gpt-5', 'catalog-key')).toBe('sonnet-5');
  });

  it('does not treat a model as selectable when its CLI is disabled or missing', () => {
    const disabledCLI: AIRuntimeCatalog = {
      ...catalog,
      clis: catalog.clis.map((cli) => cli.key === 'codex' ? { ...cli, enabled: false } : cli),
    };
    expect(isSelectableRuntimeModelValue(disabledCLI, 'codex', 'gpt-5')).toBe(false);
    expect(isSelectableRuntimePair(disabledCLI, 'codex', 'gpt-5')).toBe(false);
    expect(firstRuntimeModelValue(disabledCLI, 'codex', 'gpt-5')).toBe('');
  });

  it('shows display name and metadata, and typing filters without changing the selected value', () => {
    const { onChange } = renderModel({ cliKey: 'codex' });
    const input = screen.getByTestId('model') as HTMLInputElement;
    expect(input).toHaveValue('GPT-5');

    fireEvent.focus(input);
    expect(screen.getByTestId('model-options')).toHaveTextContent('GPT-5');
    expect(screen.getByTestId('model-options')).toHaveTextContent('frontier');
    expect(screen.getByTestId('model-options')).toHaveTextContent('400,000 ctx');
    expect(screen.getByTestId('model-options')).toHaveTextContent('in $1.25 / out $10 per MTok');

    fireEvent.change(input, { target: { value: 'does-not-exist' } });
    expect(screen.getByTestId('model-empty')).toHaveTextContent('No matching runtime models');
    expect(onChange).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: 'Escape' });
    expect(input).toHaveValue('GPT-5');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('supports keyboard navigation and selects only an existing option on Enter', () => {
    const { onChange } = renderModel({ value: '', cliKey: 'claude-code' });
    const input = screen.getByTestId('model') as HTMLInputElement;

    fireEvent.focus(input);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(onChange).toHaveBeenCalledWith('metadata-thin');
  });

  it('renders loading, empty, error, and missing-metadata states accessibly', () => {
    const refresh = vi.fn();
    const { rerender } = render(
      <RuntimeModelCombobox
        testId="model"
        ariaLabel="Model"
        isLoading
        value=""
        onChange={vi.fn()}
        onRefresh={refresh}
      />,
    );
    expect(screen.getByTestId('model-loading')).toHaveTextContent('Loading runtime catalog');

    rerender(
      <RuntimeModelCombobox
        testId="model"
        ariaLabel="Model"
        catalog={{ ...catalog, models: [] }}
        value=""
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId('model-empty')).toHaveTextContent('No runtime models available');

    rerender(
      <RuntimeModelCombobox
        testId="model"
        ariaLabel="Model"
        error={new Error('down')}
        value=""
        onChange={vi.fn()}
        onRefresh={refresh}
      />,
    );
    expect(screen.getByTestId('model-error')).toHaveTextContent('Runtime catalog unavailable');
    fireEvent.click(screen.getByTestId('model-refresh'));
    expect(refresh).toHaveBeenCalled();

    rerender(
      <RuntimeModelCombobox
        testId="model"
        ariaLabel="Model"
        catalog={catalog}
        value="legacy-custom"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId('model-missing')).toHaveTextContent('legacy-custom');
  });

  it('exposes the AI Runtime Models shortcut and refreshes when the tab regains focus', () => {
    const refresh = vi.fn();
    renderModel({ cliKey: 'codex', onRefresh: refresh, modelsHref: '/organizations/acme/ai-runtime?tab=models' });
    const input = screen.getByTestId('model') as HTMLInputElement;

    fireEvent.focus(input);
    expect(screen.getByTestId('model-models-link')).toHaveAttribute(
      'href',
      '/organizations/acme/ai-runtime?tab=models',
    );
    window.dispatchEvent(new Event('focus'));
    expect(refresh).toHaveBeenCalled();
  });

  it('renders models with missing optional metadata without crashing', () => {
    renderModel({ value: 'metadata-thin', cliKey: 'claude-code' });
    const input = screen.getByTestId('model') as HTMLInputElement;
    fireEvent.focus(input);
    expect(screen.getByTestId('model-options')).toHaveTextContent('Metadata Thin');
    expect(screen.getByTestId('model-options')).toHaveTextContent('Tier unavailable');
    expect(screen.getByTestId('model-options')).toHaveTextContent('Context unavailable');
    expect(screen.getByTestId('model-options')).toHaveTextContent('Cost unavailable');
  });
});

describe('RuntimeCLISelector', () => {
  afterEach(() => cleanup());

  it('shows CLI display names while submitting catalog keys', () => {
    const onChange = vi.fn();
    render(
      <RuntimeCLISelector
        testId="cli"
        ariaLabel="CLI"
        catalog={catalog}
        value="claude-code"
        onChange={onChange}
      />,
    );
    const select = screen.getByTestId('cli') as HTMLSelectElement;
    expect(select.options[0].textContent).toBe('Claude Code (claude-code)');
    fireEvent.change(select, { target: { value: 'codex' } });
    expect(onChange).toHaveBeenCalledWith('codex');
  });
});
