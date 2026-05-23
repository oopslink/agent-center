import type React from 'react';
import { Component } from 'react';

interface State {
  error: Error | null;
}

interface Props {
  children: React.ReactNode;
}

// Root-level ErrorBoundary catches render errors + react-query throws
// that propagate past mutations. Provides a single fallback UI with a
// reload button per F4 oversight #7. Later subtasks can wrap individual
// pages in their own boundary for finer-grained recovery.
export class ErrorBoundary extends Component<Props, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: React.ErrorInfo): void {
    if (typeof console !== 'undefined') {
      console.error('ErrorBoundary caught:', error, info);
    }
  }

  override render(): React.ReactNode {
    if (this.state.error) {
      return (
        <div className="m-6 max-w-xl rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          <p className="font-semibold">Something went wrong.</p>
          <p className="mt-1 text-red-700">{this.state.error.message}</p>
          <button
            type="button"
            className="mt-3 rounded bg-red-600 px-3 py-1 text-xs text-white hover:bg-red-700"
            onClick={() => {
              this.setState({ error: null });
              if (typeof window !== 'undefined') {
                window.location.reload();
              }
            }}
          >
            Reload
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
