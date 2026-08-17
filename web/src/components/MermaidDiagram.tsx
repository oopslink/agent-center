import type React from 'react';
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import type { MermaidConfig, RenderResult } from 'mermaid';
import { CollapsibleCodeBlock } from './CollapsibleCodeBlock';
import { IconClose, IconCopy } from './icons';
import { useModalA11y } from './useModalA11y';

export const MERMAID_RENDER_LIMITS = {
  maxChars: 12_000,
  maxLines: 300,
  maxEdges: 250,
  maxSvgChars: 420_000,
  renderTimeoutMs: 5_000,
  maxCanvasPixels: 16_000_000,
} as const;

const MIN_SCALE = 0.25;
const MAX_SCALE = 4;
const ZOOM_STEP = 1.2;
const PAN_STEP = 24;

type ThemeMode = 'light' | 'dark';

interface MermaidDiagramProps {
  code: string;
}

interface RenderedDiagram {
  svg: string;
}

interface ViewerTransform {
  scale: number;
  x: number;
  y: number;
}

interface DragState {
  pointerId: number;
  clientX: number;
  clientY: number;
  startX: number;
  startY: number;
}

// Mermaid keeps parser/configuration state at module scope. Calling initialize
// and render concurrently from several code fences can make one render disturb
// another (including leaving the caller's promise unsettled). Keep the complete
// initialize + render transaction serial while still allowing every component
// to independently publish its success/error state.
let mermaidRenderQueue: Promise<void> = Promise.resolve();

type ValidationResult =
  | { ok: true; edgeCount: number; lineCount: number }
  | { ok: false; reason: string; edgeCount: number; lineCount: number };

export function isMermaidLanguage(language?: string): boolean {
  return /^(?:mermaid|mmd)$/i.test((language ?? '').trim());
}

export function validateMermaidSource(source: string): ValidationResult {
  const normalized = source.replace(/\r\n?/g, '\n');
  const lineCount = normalized === '' ? 0 : normalized.split('\n').length;
  const edgeCount = countMermaidEdges(normalized);

  if (normalized.trim() === '') {
    return { ok: false, reason: 'Diagram source is empty.', edgeCount, lineCount };
  }
  if (normalized.length > MERMAID_RENDER_LIMITS.maxChars) {
    return {
      ok: false,
      reason: `Diagram source exceeds ${MERMAID_RENDER_LIMITS.maxChars.toLocaleString()} characters.`,
      edgeCount,
      lineCount,
    };
  }
  if (lineCount > MERMAID_RENDER_LIMITS.maxLines) {
    return {
      ok: false,
      reason: `Diagram source exceeds ${MERMAID_RENDER_LIMITS.maxLines.toLocaleString()} lines.`,
      edgeCount,
      lineCount,
    };
  }
  if (edgeCount > MERMAID_RENDER_LIMITS.maxEdges) {
    return {
      ok: false,
      reason: `Diagram source exceeds ${MERMAID_RENDER_LIMITS.maxEdges.toLocaleString()} relationships.`,
      edgeCount,
      lineCount,
    };
  }
  return { ok: true, edgeCount, lineCount };
}

export function buildMermaidConfig(themeMode: ThemeMode): MermaidConfig {
  const dark = themeMode === 'dark';
  return {
    startOnLoad: false,
    securityLevel: 'strict',
    secure: [
      'securityLevel',
      'startOnLoad',
      'maxTextSize',
      'maxEdges',
      'htmlLabels',
      'theme',
      'themeVariables',
      'themeCSS',
    ],
    maxTextSize: MERMAID_RENDER_LIMITS.maxChars,
    maxEdges: MERMAID_RENDER_LIMITS.maxEdges,
    htmlLabels: false,
    theme: 'base',
    darkMode: dark,
    look: 'classic',
    fontFamily:
      '"Plus Jakarta Sans", ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    themeVariables: dark
      ? {
          background: '#14141C',
          mainBkg: '#1e1a22',
          secondBkg: '#252128',
          primaryColor: '#252128',
          primaryTextColor: '#e3dde6',
          primaryBorderColor: '#4d4656',
          lineColor: '#a78bfa',
          textColor: '#e3dde6',
          nodeTextColor: '#e3dde6',
          edgeLabelBackground: '#1e1a22',
          clusterBkg: '#252128',
          clusterBorder: '#4d4656',
        }
      : {
          background: '#FFFCFC',
          mainBkg: '#ffffff',
          secondBkg: '#f8f5f6',
          primaryColor: '#f8f5f6',
          primaryTextColor: '#31263B',
          primaryBorderColor: '#afa9b1',
          lineColor: '#7C3AED',
          textColor: '#31263B',
          nodeTextColor: '#31263B',
          edgeLabelBackground: '#ffffff',
          clusterBkg: '#f8f5f6',
          clusterBorder: '#afa9b1',
        },
  };
}

export function validateMermaidSvg(svg: string): string | null {
  if (!/<svg[\s>]/i.test(svg)) return 'Mermaid did not return an SVG document.';
  if (svg.length > MERMAID_RENDER_LIMITS.maxSvgChars) {
    return `Rendered SVG exceeds ${MERMAID_RENDER_LIMITS.maxSvgChars.toLocaleString()} characters.`;
  }
  if (/<(?:script|iframe|object|embed|foreignObject|image)\b/i.test(svg)) {
    return 'Rendered SVG contains blocked active or external content.';
  }
  if (/\son[a-z]+\s*=/i.test(svg)) return 'Rendered SVG contains blocked event handlers.';
  if (/(?:href|xlink:href)\s*=\s*["'](?:https?:|data:|javascript:)/i.test(svg)) {
    return 'Rendered SVG contains blocked external links.';
  }
  if (/url\(\s*["']?(?:https?:|data:|javascript:)/i.test(svg)) {
    return 'Rendered SVG contains blocked external references.';
  }
  return null;
}

export function getSvgDimensions(svg: string): { width: number; height: number } {
  const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
  const svgEl = doc.querySelector('svg');
  if (!svgEl) return { width: 800, height: 600 };

  const width = parseSvgLength(svgEl.getAttribute('width'));
  const height = parseSvgLength(svgEl.getAttribute('height'));
  if (width > 0 && height > 0) return { width, height };

  const viewBox = svgEl.getAttribute('viewBox')?.trim().split(/\s+/).map(Number);
  if (viewBox && viewBox.length === 4 && Number.isFinite(viewBox[2]) && Number.isFinite(viewBox[3])) {
    return { width: Math.max(1, viewBox[2]), height: Math.max(1, viewBox[3]) };
  }
  return { width: 800, height: 600 };
}

export async function svgToPngBlob(svg: string): Promise<Blob> {
  const { width, height } = getSvgDimensions(svg);
  const scale = Math.min(2, Math.sqrt(MERMAID_RENDER_LIMITS.maxCanvasPixels / Math.max(1, width * height)));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.floor(width * scale));
  canvas.height = Math.max(1, Math.floor(height * scale));

  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('PNG export is not available in this browser.');

  const objectUrl = URL.createObjectURL(new Blob([ensureSvgXmlns(svg)], { type: 'image/svg+xml;charset=utf-8' }));
  try {
    const image = await loadImage(objectUrl);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
    return await canvasToBlob(canvas);
  } finally {
    URL.revokeObjectURL(objectUrl);
  }
}

export function MermaidDiagram({ code }: MermaidDiagramProps): React.ReactElement {
  const containerRef = useRef<HTMLDivElement>(null);
  const rawId = useId();
  const renderId = useMemo(() => `ac-mermaid-${rawId.replace(/[^A-Za-z0-9_-]/g, '')}`, [rawId]);
  const validation = useMemo(() => validateMermaidSource(code), [code]);
  const visible = useLazyVisible(containerRef, validation.ok);
  const themeMode = useThemeMode();
  const [diagram, setDiagram] = useState<RenderedDiagram | null>(null);
  const [renderError, setRenderError] = useState<string | null>(validation.ok ? null : validation.reason);
  const [loading, setLoading] = useState(false);
  const [viewerOpen, setViewerOpen] = useState(false);
  const [copyStatus, setCopyStatus] = useState('');

  useEffect(() => {
    if (!validation.ok) {
      setDiagram(null);
      setRenderError(validation.reason);
      setLoading(false);
      return;
    }
    setRenderError(null);
  }, [validation]);

  useEffect(() => {
    if (!visible || !validation.ok) return;

    let cancelled = false;
    setLoading(true);
    setRenderError(null);

    void renderMermaidDiagram(code, renderId, themeMode)
      .then((result) => {
        if (cancelled) return;
        setDiagram(result);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setDiagram(null);
        setRenderError(errorToMessage(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [code, renderId, themeMode, validation.ok, visible]);

  const copyCode = useCallback(() => {
    void copyText(code).then(
      () => showTransientStatus(setCopyStatus, 'Code copied'),
      (err: unknown) => showTransientStatus(setCopyStatus, errorToMessage(err)),
    );
  }, [code]);

  const openViewer = useCallback(() => {
    if (diagram) setViewerOpen(true);
  }, [diagram]);

  const previewInteractive = diagram != null;

  return (
    <div
      ref={containerRef}
      className="my-2 overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-1"
      data-testid="mermaid-diagram"
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base px-2 py-1.5">
        <span className="rounded bg-bg-subtle px-2 py-0.5 font-mono text-xs text-text-secondary">
          mermaid
        </span>
        <span className="ml-auto text-xs text-text-muted" aria-live="polite" data-testid="mermaid-status">
          {copyStatus || (loading ? 'Rendering diagram...' : '')}
        </span>
        <span className="flex items-center gap-1">
          <IconButton label="Copy Mermaid code" testId="mermaid-copy-code" onClick={copyCode}>
            <IconCopy />
          </IconButton>
          <IconButton
            label="Open diagram viewer"
            testId="mermaid-open-viewer"
            onClick={openViewer}
            disabled={!diagram}
          >
            <IconExpand />
          </IconButton>
        </span>
      </div>

      {renderError ? (
        <MermaidFallback code={code} error={renderError} />
      ) : (
        <div
          className={`min-h-28 overflow-auto bg-bg-base p-3 ${
            previewInteractive ? 'cursor-zoom-in' : ''
          }`}
          role={previewInteractive ? 'button' : undefined}
          tabIndex={previewInteractive ? 0 : undefined}
          aria-label={previewInteractive ? 'Open Mermaid diagram viewer' : undefined}
          aria-busy={loading}
          data-testid="mermaid-preview"
          onClick={openViewer}
          onKeyDown={(e) => {
            if (!previewInteractive) return;
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              openViewer();
            }
          }}
        >
          {diagram ? (
            <div
              className="mermaid-rendered-svg mx-auto max-h-80 max-w-full"
              data-testid="mermaid-svg"
              dangerouslySetInnerHTML={{ __html: diagram.svg }}
            />
          ) : (
            <div className="flex min-h-24 items-center justify-center text-sm text-text-muted">
              {visible ? 'Rendering diagram...' : 'Diagram preview pending.'}
            </div>
          )}
        </div>
      )}

      {viewerOpen && diagram && (
        <MermaidViewerModal code={code} svg={diagram.svg} onClose={() => setViewerOpen(false)} />
      )}
    </div>
  );
}

async function renderMermaidDiagram(
  code: string,
  id: string,
  themeMode: ThemeMode,
): Promise<RenderedDiagram> {
  return enqueueMermaidRender(async () => {
    const mermaidModule = await import('mermaid');
    const mermaid = mermaidModule.default;
    mermaid.initialize(buildMermaidConfig(themeMode));
    const rendered = await withTimeout<RenderResult>(
      mermaid.render(id, code),
      MERMAID_RENDER_LIMITS.renderTimeoutMs,
      'Mermaid render timed out.',
    );
    const svg = rendered.svg;
    const unsafeReason = validateMermaidSvg(svg);
    if (unsafeReason) throw new Error(unsafeReason);
    return { svg };
  });
}

function enqueueMermaidRender<T>(render: () => Promise<T>): Promise<T> {
  const result = mermaidRenderQueue.then(render, render);
  mermaidRenderQueue = result.then(
    () => undefined,
    () => undefined,
  );
  return result;
}

function MermaidFallback({ code, error }: { code: string; error: string }): React.ReactElement {
  return (
    <div className="space-y-2 p-3" data-testid="mermaid-error">
      <div className="rounded border border-status-amber-border bg-status-amber-bg px-3 py-2 text-sm text-status-amber-fg">
        Mermaid diagram was not rendered: {error}
      </div>
      <CollapsibleCodeBlock code={code} language="mermaid" contextLabel="code" />
    </div>
  );
}

function MermaidViewerModal({
  code,
  svg,
  onClose,
}: {
  code: string;
  svg: string;
  onClose: () => void;
}): React.ReactElement {
  const containerRef = useModalA11y({ open: true, onClose });
  const viewportRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<DragState | null>(null);
  const [transform, setTransform] = useState<ViewerTransform>({ scale: 1, x: 0, y: 0 });
  const [status, setStatus] = useState('');

  const zoom = useCallback((factor: number) => {
    setTransform((current) => ({ ...current, scale: clampScale(current.scale * factor) }));
  }, []);

  const reset = useCallback(() => {
    setTransform({ scale: 1, x: 0, y: 0 });
  }, []);

  const fit = useCallback(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const { width, height } = getSvgDimensions(svg);
    const availableWidth = Math.max(1, viewport.clientWidth - 32);
    const availableHeight = Math.max(1, viewport.clientHeight - 32);
    const scale = clampScale(Math.min(availableWidth / width, availableHeight / height));
    setTransform({ scale, x: 0, y: 0 });
  }, [svg]);

  const copyCode = useCallback(() => {
    void copyText(code).then(
      () => showTransientStatus(setStatus, 'Code copied'),
      (err: unknown) => showTransientStatus(setStatus, errorToMessage(err)),
    );
  }, [code]);

  const copyPng = useCallback(() => {
    void copySvgPng(svg).then(
      () => showTransientStatus(setStatus, 'PNG copied'),
      (err: unknown) => showTransientStatus(setStatus, errorToMessage(err)),
    );
  }, [svg]);

  const downloadSvg = useCallback(() => {
    downloadBlob(new Blob([ensureSvgXmlns(svg)], { type: 'image/svg+xml;charset=utf-8' }), 'mermaid-diagram.svg');
    showTransientStatus(setStatus, 'SVG downloaded');
  }, [svg]);

  const downloadPng = useCallback(() => {
    void svgToPngBlob(svg).then(
      (blob) => {
        downloadBlob(blob, 'mermaid-diagram.png');
        showTransientStatus(setStatus, 'PNG downloaded');
      },
      (err: unknown) => showTransientStatus(setStatus, errorToMessage(err)),
    );
  }, [svg]);

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-30 flex items-center justify-center bg-black/60 p-2 sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="mermaid-viewer-title"
      data-testid="mermaid-viewer-modal"
    >
      <div className="flex h-[92dvh] w-full max-w-6xl flex-col overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-3">
        <div className="flex flex-wrap items-center gap-2 border-b border-border-base px-3 py-2">
          <h2 id="mermaid-viewer-title" className="mr-auto text-sm font-semibold">
            Mermaid diagram
          </h2>
          <span className="text-xs text-text-muted" aria-live="polite" data-testid="mermaid-viewer-status">
            {status || `${Math.round(transform.scale * 100)}%`}
          </span>
          <IconButton label="Zoom out" testId="mermaid-zoom-out" onClick={() => zoom(1 / ZOOM_STEP)}>
            <IconZoomOut />
          </IconButton>
          <IconButton label="Zoom in" testId="mermaid-zoom-in" onClick={() => zoom(ZOOM_STEP)}>
            <IconZoomIn />
          </IconButton>
          <IconButton label="Fit diagram" testId="mermaid-fit" onClick={fit}>
            <IconFit />
          </IconButton>
          <IconButton label="Reset view" testId="mermaid-reset" onClick={reset}>
            <IconReset />
          </IconButton>
          <IconButton label="Copy Mermaid code" testId="mermaid-viewer-copy-code" onClick={copyCode}>
            <IconCopy />
          </IconButton>
          <IconButton label="Copy PNG" testId="mermaid-copy-png" onClick={copyPng}>
            <IconImage />
          </IconButton>
          <IconButton label="Download PNG" testId="mermaid-download-png" onClick={downloadPng}>
            <IconDownload />
          </IconButton>
          <IconButton label="Download SVG" testId="mermaid-download-svg" onClick={downloadSvg}>
            <IconSvg />
          </IconButton>
          <IconButton label="Close diagram viewer" testId="mermaid-close-viewer" onClick={onClose}>
            <IconClose />
          </IconButton>
        </div>

        <div
          ref={viewportRef}
          className="relative flex-1 overflow-hidden bg-bg-base touch-none"
          data-testid="mermaid-viewer-viewport"
          tabIndex={0}
          aria-label="Mermaid diagram canvas"
          onPointerDown={(e) => {
            dragRef.current = {
              pointerId: e.pointerId,
              clientX: e.clientX,
              clientY: e.clientY,
              startX: transform.x,
              startY: transform.y,
            };
            e.currentTarget.setPointerCapture?.(e.pointerId);
          }}
          onPointerMove={(e) => {
            const drag = dragRef.current;
            if (!drag || drag.pointerId !== e.pointerId) return;
            setTransform((current) => ({
              ...current,
              x: drag.startX + e.clientX - drag.clientX,
              y: drag.startY + e.clientY - drag.clientY,
            }));
          }}
          onPointerUp={(e) => {
            if (dragRef.current?.pointerId === e.pointerId) dragRef.current = null;
            e.currentTarget.releasePointerCapture?.(e.pointerId);
          }}
          onPointerCancel={(e) => {
            if (dragRef.current?.pointerId === e.pointerId) dragRef.current = null;
          }}
          onKeyDown={(e) => {
            if (e.key === '+' || e.key === '=') {
              e.preventDefault();
              zoom(ZOOM_STEP);
            } else if (e.key === '-') {
              e.preventDefault();
              zoom(1 / ZOOM_STEP);
            } else if (e.key === '0') {
              e.preventDefault();
              reset();
            } else if (e.key.toLowerCase() === 'f') {
              e.preventDefault();
              fit();
            } else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'ArrowUp' || e.key === 'ArrowDown') {
              e.preventDefault();
              setTransform((current) => ({
                ...current,
                x: current.x + (e.key === 'ArrowLeft' ? -PAN_STEP : e.key === 'ArrowRight' ? PAN_STEP : 0),
                y: current.y + (e.key === 'ArrowUp' ? -PAN_STEP : e.key === 'ArrowDown' ? PAN_STEP : 0),
              }));
            }
          }}
        >
          <div
            className="mermaid-viewer-svg absolute left-1/2 top-1/2 max-w-none origin-center"
            data-testid="mermaid-viewer-transform"
            style={{
              transform: `translate(calc(-50% + ${transform.x}px), calc(-50% + ${transform.y}px)) scale(${transform.scale})`,
            }}
            dangerouslySetInnerHTML={{ __html: svg }}
          />
        </div>
      </div>
    </div>
  );
}

function IconButton({
  label,
  testId,
  disabled = false,
  onClick,
  children,
}: {
  label: string;
  testId: string;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      className="inline-flex h-8 w-8 items-center justify-center rounded border border-border-base text-text-secondary hover:bg-bg-subtle hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-50"
      aria-label={label}
      title={label}
      data-testid={testId}
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
    >
      {children}
    </button>
  );
}

function useLazyVisible(ref: React.RefObject<Element | null>, enabled: boolean): boolean {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (!enabled || visible) return;
    const el = ref.current;
    if (!el) return;
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { rootMargin: '180px' },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [enabled, ref, visible]);

  return visible;
}

function useThemeMode(): ThemeMode {
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => readThemeMode());

  useEffect(() => {
    if (typeof MutationObserver === 'undefined' || typeof document === 'undefined') return;
    const observer = new MutationObserver(() => setThemeMode(readThemeMode()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);

  return themeMode;
}

function readThemeMode(): ThemeMode {
  if (typeof document === 'undefined') return 'light';
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light';
}

function countMermaidEdges(source: string): number {
  const matches = source.match(/(?:-{1,3}|={1,3}|\.{1,3})(?:[ox])?(?:>|-)|(?:->>|-->>|--x|--\))/g);
  return matches?.length ?? 0;
}

function parseSvgLength(value: string | null): number {
  if (!value) return 0;
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function clampScale(value: number): number {
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, value));
}

function ensureSvgXmlns(svg: string): string {
  if (/^<svg\b[^>]*\sxmlns=/i.test(svg.trim())) return svg;
  return svg.replace(/^<svg\b/i, '<svg xmlns="http://www.w3.org/2000/svg"');
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error('SVG image could not be loaded for PNG export.'));
    image.src = src;
  });
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new Error('PNG export failed.'));
    }, 'image/png');
  });
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, message: string): Promise<T> {
  let timeoutId = 0;
  const timeout = new Promise<never>((_, reject) => {
    timeoutId = window.setTimeout(() => reject(new Error(message)), timeoutMs);
  });
  return Promise.race([promise, timeout]).finally(() => window.clearTimeout(timeoutId));
}

function errorToMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim() !== '') return err.message;
  if (typeof err === 'string' && err.trim() !== '') return err;
  return 'Unknown Mermaid rendering error.';
}

async function copyText(text: string): Promise<void> {
  if (!navigator.clipboard?.writeText) throw new Error('Clipboard text copy is not available.');
  await navigator.clipboard.writeText(text);
}

async function copySvgPng(svg: string): Promise<void> {
  if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
    throw new Error('PNG clipboard copy is not available.');
  }
  const png = await svgToPngBlob(svg);
  await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })]);
}

function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

function showTransientStatus(setStatus: React.Dispatch<React.SetStateAction<string>>, message: string): void {
  setStatus(message);
  window.setTimeout(() => setStatus(''), 2_000);
}

function IconExpand(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M15 3h6v6" />
      <path d="M21 3l-7 7" />
      <path d="M9 21H3v-6" />
      <path d="M3 21l7-7" />
    </svg>
  );
}

function IconZoomIn(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="M11 8v6M8 11h6M20 20l-3.5-3.5" />
    </svg>
  );
}

function IconZoomOut(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="M8 11h6M20 20l-3.5-3.5" />
    </svg>
  );
}

function IconFit(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
    </svg>
  );
}

function IconReset(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 12a9 9 0 1 0 3-6.7" />
      <path d="M3 4v6h6" />
    </svg>
  );
}

function IconImage(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M8 11a2 2 0 1 0 0-4 2 2 0 0 0 0 4M21 15l-5-5L5 19" />
    </svg>
  );
}

function IconDownload(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3v12" />
      <path d="M7 10l5 5 5-5" />
      <path d="M5 21h14" />
    </svg>
  );
}

function IconSvg(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M7 3h7l5 5v13H7z" />
      <path d="M14 3v5h5" />
      <path d="M5 7H3v14h12v-2" />
    </svg>
  );
}
