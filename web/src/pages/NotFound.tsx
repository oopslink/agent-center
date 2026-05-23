import type React from 'react';
import { Link } from 'react-router-dom';

// NotFound — 404 with a navigation link back to the home so users don't
// get stuck. Per x9527 #6 open question 5.
export default function NotFound(): React.ReactElement {
  return (
    <section className="space-y-4" data-testid="page-NotFound">
      <h2 className="text-xl font-semibold">404 — Not found</h2>
      <p className="text-sm text-slate-500">
        The page you requested does not exist.
      </p>
      <div className="space-x-3">
        <Link
          to="/channels"
          className="text-blue-600 hover:underline"
          data-testid="nav-home"
        >
          Back to channels
        </Link>
        <Link to="/fleet" className="text-blue-600 hover:underline">
          Fleet overview
        </Link>
      </div>
    </section>
  );
}
