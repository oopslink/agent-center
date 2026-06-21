import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { useListControls, SortHeader, Pagination } from './listControls';

describe('useListControls', () => {
  afterEach(() => cleanup());

  it('defaults to the given sort/dir and page 1', () => {
    const { result } = renderHook(() => useListControls({ defaultSort: 'updated_at', defaultDir: 'desc', pageSize: 25 }));
    expect(result.current.sort).toBe('updated_at');
    expect(result.current.dir).toBe('desc');
    expect(result.current.page).toBe(1);
    expect(result.current.pageSize).toBe(25);
  });

  it('toggleSort flips direction on the same key and resets the page', () => {
    const { result } = renderHook(() => useListControls({ defaultSort: 'updated_at', defaultDir: 'desc' }));
    act(() => result.current.setPage(3));
    expect(result.current.page).toBe(3);
    act(() => result.current.toggleSort('updated_at')); // same key → flip desc→asc, reset page
    expect(result.current.dir).toBe('asc');
    expect(result.current.page).toBe(1);
  });

  it('toggleSort selects a new key (time keys default desc, others asc)', () => {
    const { result } = renderHook(() => useListControls());
    act(() => result.current.toggleSort('title'));
    expect(result.current.sort).toBe('title');
    expect(result.current.dir).toBe('asc');
    act(() => result.current.toggleSort('created_at'));
    expect(result.current.sort).toBe('created_at');
    expect(result.current.dir).toBe('desc');
  });
});

describe('Pagination', () => {
  afterEach(() => cleanup());

  it('renders nothing when everything fits on one page', () => {
    const { container } = render(
      <Pagination page={1} pageSize={25} total={10} onPageChange={() => {}} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('shows range + page count and disables Prev on the first page', () => {
    render(<Pagination page={1} pageSize={25} total={60} onPageChange={() => {}} />);
    expect(screen.getByTestId('pagination-range')).toHaveTextContent('1–25 of 60');
    expect(screen.getByTestId('pagination-page')).toHaveTextContent('Page 1 / 3');
    expect(screen.getByTestId('pagination-prev')).toBeDisabled();
    expect(screen.getByTestId('pagination-next')).not.toBeDisabled();
  });

  it('Next/Prev call onPageChange; Next disabled on the last page', () => {
    let page = 3;
    const onPageChange = (p: number) => {
      page = p;
    };
    const { rerender } = render(
      <Pagination page={3} pageSize={25} total={60} onPageChange={onPageChange} />,
    );
    expect(screen.getByTestId('pagination-next')).toBeDisabled();
    fireEvent.click(screen.getByTestId('pagination-prev'));
    expect(page).toBe(2);
    rerender(<Pagination page={2} pageSize={25} total={60} onPageChange={onPageChange} />);
    fireEvent.click(screen.getByTestId('pagination-next'));
    expect(page).toBe(3);
  });
});

describe('SortHeader', () => {
  afterEach(() => cleanup());

  it('renders a sort button and toggles via the controls', () => {
    function Harness(): React.ReactElement {
      const controls = useListControls({ defaultSort: 'updated_at', defaultDir: 'desc' });
      return (
        <table>
          <thead>
            <tr>
              <SortHeader label="Title" sortKey="title" controls={controls} />
              <SortHeader label="Updated" sortKey="updated_at" controls={controls} />
            </tr>
          </thead>
        </table>
      );
    }
    render(<Harness />);
    // updated_at is the active default column.
    expect(screen.getByTestId('sort-updated_at')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('sort-title')).toHaveAttribute('data-active', 'false');
    // clicking title makes it active.
    fireEvent.click(screen.getByTestId('sort-title'));
    expect(screen.getByTestId('sort-title')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('sort-updated_at')).toHaveAttribute('data-active', 'false');
  });
});
