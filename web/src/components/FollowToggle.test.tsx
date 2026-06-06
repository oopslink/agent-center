import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, fireEvent } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { FollowToggle } from './FollowToggle';

// v2.8 #264 P1 / #176 §4: a conversation header follow/unfollow control.
describe('FollowToggle (#176 §4)', () => {
  afterEach(() => cleanup());

  it('not-following → "Follow" affordance (aria-pressed=false), click POSTs /follow', async () => {
    let posted = false;
    server.use(
      http.post('/api/conversations/:id/follow', () => {
        posted = true;
        return HttpResponse.json({ followed: true });
      }),
    );
    render(<FollowToggle conversationId="C1" followed={false} />, { wrapper: makeWrapper() });
    const btn = screen.getByTestId('follow-toggle');
    expect(btn).toHaveAttribute('aria-pressed', 'false');
    expect(btn).toHaveAccessibleName(/follow/i);
    fireEvent.click(btn);
    await waitFor(() => expect(posted).toBe(true));
  });

  it('following → "Following" affordance (aria-pressed=true), click DELETEs /follow', async () => {
    let deleted = false;
    server.use(
      http.delete('/api/conversations/:id/follow', () => {
        deleted = true;
        return HttpResponse.json({ followed: false });
      }),
    );
    render(<FollowToggle conversationId="C1" followed={true} />, { wrapper: makeWrapper() });
    const btn = screen.getByTestId('follow-toggle');
    expect(btn).toHaveAttribute('aria-pressed', 'true');
    expect(btn).toHaveAccessibleName(/unfollow|following/i);
    fireEvent.click(btn);
    await waitFor(() => expect(deleted).toBe(true));
  });
});
