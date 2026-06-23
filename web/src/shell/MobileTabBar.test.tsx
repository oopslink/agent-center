import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { MobileTabBar, type TabBarModule } from './MobileTabBar';

function Dot(): React.ReactElement {
  return <svg viewBox="0 0 20 20" aria-hidden="true" />;
}

const MODULES: ReadonlyArray<TabBarModule> = [
  { id: 'workspace', label: 'Workspace', short: 'Work', defaultPath: 'projects', Icon: Dot },
  { id: 'conversations', label: 'Conversations', short: 'Chat', defaultPath: 'channels', Icon: Dot },
  { id: 'members', label: 'Members', short: 'Members', defaultPath: 'members/humans', Icon: Dot },
  { id: 'system', label: 'System', short: 'System', defaultPath: 'environment', Icon: Dot },
];

function renderBar(
  activeModuleId?: TabBarModule['id'],
  orgBase = '',
  badge?: { unread?: number; mentions?: number },
) {
  return render(
    <MemoryRouter>
      <MobileTabBar
        modules={MODULES}
        activeModuleId={activeModuleId}
        orgBase={orgBase}
        conversationsUnread={badge?.unread}
        conversationsMentions={badge?.mentions}
      />
    </MemoryRouter>,
  );
}

describe('MobileTabBar (v2.10.1 [M1] mobile bottom nav)', () => {
  afterEach(() => cleanup());

  it('renders the four module tabs linking to their default pages', () => {
    renderBar('conversations');
    const bar = screen.getByTestId('mobile-tabbar');
    expect(within(bar).getByTestId('tab-workspace')).toHaveAttribute('href', '/projects');
    expect(within(bar).getByTestId('tab-conversations')).toHaveAttribute('href', '/channels');
    expect(within(bar).getByTestId('tab-members')).toHaveAttribute('href', '/members/humans');
    expect(within(bar).getByTestId('tab-system')).toHaveAttribute('href', '/environment');
  });

  it('org-prefixes the hrefs when an org base is supplied', () => {
    renderBar('workspace', '/organizations/acme');
    expect(screen.getByTestId('tab-workspace')).toHaveAttribute('href', '/organizations/acme/projects');
  });

  it('marks the active module and aria-current="page"', () => {
    renderBar('members');
    expect(screen.getByTestId('tab-members')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('tab-members')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('tab-system')).toHaveAttribute('data-active', 'false');
  });

  it('every tab is a ≥44px touch target', () => {
    renderBar();
    for (const m of MODULES) {
      expect(screen.getByTestId(`tab-${m.id}`).className).toContain('min-h-[44px]');
    }
  });

  // T343: cross-source unread badge on the Chat tab (mirrors the desktop rail).
  it('shows no Chat-tab unread badge at zero unread', () => {
    renderBar('workspace', '', { unread: 0, mentions: 0 });
    expect(screen.queryByTestId('tab-conversations-unread-badge')).toBeNull();
  });

  it('shows the unread count on the Chat tab (neutral when no mentions)', () => {
    renderBar('workspace', '', { unread: 5, mentions: 0 });
    const badge = screen.getByTestId('tab-conversations-unread-badge');
    expect(badge).toHaveTextContent('5');
    expect(badge).toHaveAttribute('data-mention', 'false');
  });

  it('shows the @me mention total (brand) when any source mentions me, capped at 99+', () => {
    renderBar('workspace', '', { unread: 120, mentions: 3 });
    const badge = screen.getByTestId('tab-conversations-unread-badge');
    expect(badge).toHaveTextContent('99+'); // count capped
    expect(badge).toHaveAttribute('data-mention', 'true');
  });
});
