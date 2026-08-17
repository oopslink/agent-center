import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import {
  MERMAID_RENDER_LIMITS,
  MermaidDiagram,
  buildMermaidConfig,
  isMermaidLanguage,
  validateMermaidSource,
  validateMermaidSvg,
} from './MermaidDiagram';

const mermaidMock = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
}));

vi.mock('mermaid', () => ({ default: mermaidMock }));

const simpleCode = 'graph TD\n  A --> B';
const safeSvg = '<svg viewBox="0 0 200 100"><g><text>A to B</text></g></svg>';

let intersectionCallback: IntersectionObserverCallback | null = null;

class TestIntersectionObserver implements IntersectionObserver {
  readonly root = null;
  readonly rootMargin = '180px';
  readonly thresholds = [];
  disconnect = vi.fn();
  observe = vi.fn();
  takeRecords = () => [];
  unobserve = vi.fn();

  constructor(callback: IntersectionObserverCallback) {
    intersectionCallback = callback;
  }
}

function revealDiagram(): void {
  act(() => {
    intersectionCallback?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver);
  });
}

describe('MermaidDiagram', () => {
  beforeEach(() => {
    intersectionCallback = null;
    vi.stubGlobal('IntersectionObserver', TestIntersectionObserver);
    Object.assign(navigator, {
      clipboard: {
        writeText: vi.fn().mockResolvedValue(undefined),
        write: vi.fn().mockResolvedValue(undefined),
      },
    });
    document.documentElement.className = '';
    mermaidMock.initialize.mockClear();
    mermaidMock.render.mockReset();
    mermaidMock.render.mockResolvedValue({ svg: safeSvg });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('recognizes mermaid fenced language aliases only', () => {
    expect(isMermaidLanguage('mermaid')).toBe(true);
    expect(isMermaidLanguage('MMD')).toBe(true);
    expect(isMermaidLanguage('typescript')).toBe(false);
    expect(isMermaidLanguage(undefined)).toBe(false);
  });

  it('validates input complexity before Mermaid is loaded', () => {
    expect(validateMermaidSource(simpleCode)).toMatchObject({ ok: true, lineCount: 2, edgeCount: 1 });
    expect(validateMermaidSource('')).toMatchObject({ ok: false, reason: 'Diagram source is empty.' });
    expect(validateMermaidSource(`graph TD\n${'A --> B\n'.repeat(MERMAID_RENDER_LIMITS.maxLines)}`)).toMatchObject({
      ok: false,
      reason: expect.stringContaining('lines'),
    });
    expect(validateMermaidSource(`graph TD\n${'A --> B\n'.repeat(MERMAID_RENDER_LIMITS.maxEdges + 1)}`)).toMatchObject({
      ok: false,
      reason: expect.stringContaining('relationships'),
    });
  });

  it('builds a strict, locked Mermaid config for light and dark mode', () => {
    const light = buildMermaidConfig('light');
    const dark = buildMermaidConfig('dark');
    expect(light).toMatchObject({
      startOnLoad: false,
      securityLevel: 'strict',
      maxTextSize: MERMAID_RENDER_LIMITS.maxChars,
      maxEdges: MERMAID_RENDER_LIMITS.maxEdges,
      htmlLabels: false,
      theme: 'base',
      darkMode: false,
    });
    expect(light.secure).toEqual(
      expect.arrayContaining(['securityLevel', 'startOnLoad', 'maxTextSize', 'maxEdges', 'htmlLabels', 'theme']),
    );
    expect(dark.darkMode).toBe(true);
    expect(dark.themeVariables).not.toEqual(light.themeVariables);
  });

  it('lazy-loads and renders only after the preview becomes visible', async () => {
    render(<MermaidDiagram code={simpleCode} />);
    expect(mermaidMock.render).not.toHaveBeenCalled();

    revealDiagram();

    await waitFor(() => expect(mermaidMock.render).toHaveBeenCalledWith(expect.stringMatching(/^ac-mermaid-/), simpleCode));
    expect(mermaidMock.initialize).toHaveBeenCalledWith(
      expect.objectContaining({
        securityLevel: 'strict',
        startOnLoad: false,
        maxTextSize: MERMAID_RENDER_LIMITS.maxChars,
        maxEdges: MERMAID_RENDER_LIMITS.maxEdges,
      }),
    );
    expect(screen.getByTestId('mermaid-svg')).toHaveTextContent('A to B');
  });

  it('falls back to raw code when source exceeds limits', () => {
    render(<MermaidDiagram code={`graph TD\n${'A --> B\n'.repeat(MERMAID_RENDER_LIMITS.maxLines)}`} />);
    expect(screen.getByTestId('mermaid-error')).toHaveTextContent('Mermaid diagram was not rendered');
    expect(screen.getByTestId('collapsible-code-block')).toBeInTheDocument();
    expect(mermaidMock.render).not.toHaveBeenCalled();
  });

  it('falls back to raw code on Mermaid render errors', async () => {
    mermaidMock.render.mockRejectedValueOnce(new Error('bad syntax'));
    render(<MermaidDiagram code={simpleCode} />);
    revealDiagram();

    await waitFor(() => expect(screen.getByTestId('mermaid-error')).toHaveTextContent('bad syntax'));
    expect(screen.getByTestId('collapsible-code-block')).toBeInTheDocument();
  });

  it('blocks unsafe SVG output before inserting it into the DOM', async () => {
    expect(validateMermaidSvg('<svg><script>alert(1)</script></svg>')).toContain('blocked');
    mermaidMock.render.mockResolvedValueOnce({ svg: '<svg><script>window.__pwn=1</script></svg>' });
    render(<MermaidDiagram code={simpleCode} />);
    revealDiagram();

    await waitFor(() => expect(screen.getByTestId('mermaid-error')).toHaveTextContent('blocked'));
    expect(screen.queryByText('window.__pwn=1')).toBeNull();
    expect(document.querySelector('script')).toBeNull();
  });

  it('opens an accessible viewer with zoom, pan, reset and keyboard controls', async () => {
    render(<MermaidDiagram code={simpleCode} />);
    revealDiagram();
    await waitFor(() => expect(screen.getByTestId('mermaid-open-viewer')).not.toBeDisabled());

    fireEvent.click(screen.getByTestId('mermaid-open-viewer'));
    const modal = screen.getByTestId('mermaid-viewer-modal');
    expect(modal).toHaveAttribute('role', 'dialog');
    expect(modal).toHaveAttribute('aria-modal', 'true');

    const transform = screen.getByTestId('mermaid-viewer-transform');
    fireEvent.click(screen.getByTestId('mermaid-zoom-in'));
    expect(transform).toHaveStyle({ transform: 'translate(calc(-50% + 0px), calc(-50% + 0px)) scale(1.2)' });

    const viewport = screen.getByTestId('mermaid-viewer-viewport');
    fireEvent.pointerDown(viewport, { pointerId: 1, clientX: 10, clientY: 10 });
    fireEvent.pointerMove(viewport, { pointerId: 1, clientX: 34, clientY: 40 });
    expect(transform).toHaveStyle({ transform: 'translate(calc(-50% + 24px), calc(-50% + 30px)) scale(1.2)' });

    fireEvent.keyDown(viewport, { key: 'ArrowLeft' });
    expect(transform).toHaveStyle({ transform: 'translate(calc(-50% + 0px), calc(-50% + 30px)) scale(1.2)' });

    fireEvent.click(screen.getByTestId('mermaid-reset'));
    expect(transform).toHaveStyle({ transform: 'translate(calc(-50% + 0px), calc(-50% + 0px)) scale(1)' });

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId('mermaid-viewer-modal')).toBeNull();
  });

  it('copies Mermaid code from preview and viewer controls', async () => {
    render(<MermaidDiagram code={simpleCode} />);
    fireEvent.click(screen.getByTestId('mermaid-copy-code'));
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(simpleCode));

    revealDiagram();
    await waitFor(() => expect(screen.getByTestId('mermaid-open-viewer')).not.toBeDisabled());
    fireEvent.click(screen.getByTestId('mermaid-open-viewer'));
    fireEvent.click(screen.getByTestId('mermaid-viewer-copy-code'));
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(2));
  });

  it('copies PNG and downloads PNG/SVG from the viewer', async () => {
    installPngExportMocks();
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    render(<MermaidDiagram code={simpleCode} />);
    revealDiagram();
    await waitFor(() => expect(screen.getByTestId('mermaid-open-viewer')).not.toBeDisabled());
    fireEvent.click(screen.getByTestId('mermaid-open-viewer'));

    fireEvent.click(screen.getByTestId('mermaid-copy-png'));
    await waitFor(() => expect(navigator.clipboard.write).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByTestId('mermaid-download-svg'));
    expect(anchorClick).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByTestId('mermaid-download-png'));
    await waitFor(() => expect(anchorClick).toHaveBeenCalledTimes(2));
  });
});

function installPngExportMocks(): void {
  class TestClipboardItem {
    constructor(readonly items: Record<string, Blob>) {}
  }
  class TestImage {
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;

    set src(_value: string) {
      window.setTimeout(() => this.onload?.(), 0);
    }
  }

  vi.stubGlobal('ClipboardItem', TestClipboardItem);
  vi.stubGlobal('Image', TestImage);
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mermaid');
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(
    () =>
      ({
        clearRect: vi.fn(),
        drawImage: vi.fn(),
      }) as unknown as CanvasRenderingContext2D,
  );
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation((callback: BlobCallback) => {
    callback(new Blob(['png'], { type: 'image/png' }));
  });
}
