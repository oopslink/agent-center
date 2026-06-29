// Shared code-repo display helpers (T575, issue-f980c8de). One place for the
// provider badge + the credential-mask display so the workspace Repos page and
// the project referencer render repos identically.
import type React from 'react';

// Known providers (mirrors the backend's coderepo.Provider set; free text is
// tolerated — an unknown provider renders with the neutral fallback style).
export const REPO_PROVIDERS = ['github', 'git'] as const;

// Color-code the provider badge: GitHub gets the slate chip, generic git the
// green chip (per mockup). Uses the shared status-chip palette (a11y tokens).
function providerBadgeClass(provider: string): string {
  switch (provider) {
    case 'github':
      return 'bg-status-slate-bg text-status-slate-fg';
    case 'git':
      return 'bg-status-green-bg text-status-green-fg';
    default:
      return 'bg-status-slate-bg text-status-slate-fg';
  }
}

export function ProviderBadge({ provider }: { provider: string }): React.ReactElement {
  return (
    <span
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide ${providerBadgeClass(provider)}`}
      data-testid="repo-provider-badge"
    >
      {provider || 'git'}
    </span>
  );
}
