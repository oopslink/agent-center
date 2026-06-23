import { afterEach, describe, expect, it } from 'vitest';
import { render, cleanup, screen } from '@testing-library/react';
import { OverviewCards } from './OverviewCards';
import type { CardData } from '@/utils/analyticsWindows';

// The mockup's exact card figures (docs/design/v2.15.0/mockups/i28-analytics-en.png).
const MOCKUP: CardData = {
  tokens: 48_200_000,
  tokensDeltaPct: 12.4,
  costMicros: 312_740_000,
  costDeltaPct: 8.1,
  tasksDone: 37,
  tasksDoneDelta: -3,
  activeDays: 23,
  activeDenom: 30,
  activeRatePct: 77,
  streakCurrent: 9,
  streakLongest: 14,
};

describe('OverviewCards', () => {
  afterEach(() => cleanup());

  it('renders the five cards 1:1 with the mockup', () => {
    render(<OverviewCards cards={MOCKUP} />);
    expect(screen.getByTestId('card-tokens-value')).toHaveTextContent('48.2M');
    expect(screen.getByTestId('card-tokens-delta')).toHaveTextContent('+12.4%');
    expect(screen.getByTestId('card-cost-value')).toHaveTextContent('$312.74');
    expect(screen.getByTestId('card-cost-delta')).toHaveTextContent('+8.1%');
    expect(screen.getByTestId('card-tasks-value')).toHaveTextContent('37');
    expect(screen.getByTestId('card-tasks-delta')).toHaveTextContent('-3');
    expect(screen.getByTestId('card-active-value')).toHaveTextContent('23/30');
    expect(screen.getByTestId('card-active-rate')).toHaveTextContent('77% active rate');
    expect(screen.getByTestId('card-streak-value')).toHaveTextContent('9 days');
    expect(screen.getByTestId('card-streak-longest')).toHaveTextContent('longest 14 days');
  });

  it('shows a green up-delta and a red down-delta via color tokens', () => {
    render(<OverviewCards cards={MOCKUP} />);
    expect(screen.getByTestId('card-tokens-delta').className).toContain('text-success');
    expect(screen.getByTestId('card-tasks-delta').className).toContain('text-danger');
  });

  it('renders a flat 0% delta with neutral styling', () => {
    render(<OverviewCards cards={{ ...MOCKUP, tokensDeltaPct: 0 }} />);
    const chip = screen.getByTestId('card-tokens-delta');
    expect(chip).toHaveTextContent('0%');
    expect(chip.className).toContain('text-text-muted');
  });
});
