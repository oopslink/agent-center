import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MarkdownMessage } from './MarkdownMessage';

// v2.8 #276 — markdown rendering for message content (@oopslink chose A = full
// react-markdown + GFM + strict escape, no rehype-raw). Fenced code blocks
// render through the shared CollapsibleCodeBlock; raw HTML is neutralized.
describe('MarkdownMessage (#276)', () => {
  afterEach(() => cleanup());

  it('renders markdown structure (heading, bold, list)', () => {
    render(<MarkdownMessage content={'# Title\n\nsome **bold** text\n\n- a\n- b'} />);
    const root = screen.getByTestId('markdown-message');
    expect(root.querySelector('h1')).toHaveTextContent('Title');
    expect(root.querySelector('strong')).toHaveTextContent('bold');
    expect(root.querySelectorAll('li')).toHaveLength(2);
  });

  it('renders a fenced code block through CollapsibleCodeBlock (with language)', () => {
    const code = Array.from({ length: 25 }, (_, i) => `row ${i + 1}`).join('\n');
    render(<MarkdownMessage content={'```ts\n' + code + '\n```'} />);
    // the shared collapsible block (over threshold → has the disclosure).
    expect(screen.getByTestId('collapsible-code-block')).toBeInTheDocument();
    expect(screen.getByTestId('code-lang-badge')).toHaveTextContent('ts');
    expect(screen.getByTestId('code-disclosure-btn')).toBeInTheDocument();
  });

  it('renders a fenced block WITHOUT a language through CollapsibleCodeBlock too', () => {
    render(<MarkdownMessage content={'```\nplain fenced\n```'} />);
    expect(screen.getByTestId('collapsible-code-block')).toBeInTheDocument();
  });

  it('renders inline code as a plain <code>, NOT a collapsible block', () => {
    render(<MarkdownMessage content={'use `npm install` here'} />);
    expect(screen.queryByTestId('collapsible-code-block')).toBeNull();
    expect(screen.getByTestId('markdown-message').querySelector('code')).toHaveTextContent('npm install');
  });

  it('renders GFM tables as real table/th/td (remark-gfm; v2.8.1 .markdown-body CSS targets these)', () => {
    render(<MarkdownMessage content={'| a | b |\n|---|---|\n| 1 | 2 |'} />);
    const table = screen.getByTestId('markdown-message').querySelector('table');
    expect(table).toBeInTheDocument();
    expect(table?.querySelectorAll('th')).toHaveLength(2);
    // v2.8.1 polish styles th + td; assert the body cells render so the CSS has a target.
    expect(table?.querySelectorAll('td')).toHaveLength(2);
  });

  it('adds rel="noopener noreferrer" to links', () => {
    render(<MarkdownMessage content={'[link](https://example.com)'} />);
    const a = screen.getByTestId('markdown-message').querySelector('a');
    expect(a?.getAttribute('href')).toContain('example.com');
    expect(a).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  // SECURITY GATE: raw HTML is escaped (no rehype-raw) — <script> never becomes
  // an element; a javascript: link is neutralized by the default url transform.
  it('neutralizes raw HTML <script> (no rehype-raw)', () => {
    render(<MarkdownMessage content={'<script>window.__xss=1</script>\n\nsafe'} />);
    const root = screen.getByTestId('markdown-message');
    expect(root.querySelector('script')).toBeNull();
  });

  it('strips a javascript: link href (dangerous scheme)', () => {
    render(<MarkdownMessage content={'[click](javascript:alert(1))'} />);
    const a = screen.getByTestId('markdown-message').querySelector('a');
    // react-markdown's default urlTransform drops dangerous protocols → empty/safe href.
    expect(a?.getAttribute('href') ?? '').not.toContain('javascript:');
  });
});
