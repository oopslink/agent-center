// v2.8.1 (@oopslink): syntax highlighting for CollapsibleCodeBlock, pulled
// forward from v2.9. This module is imported LAZILY (dynamic import) by the
// component so highlight.js + its grammars stay out of the main/route bundle
// until a code block with a known language actually renders.
//
// Security: highlight.js ESCAPES the input — `hljs.highlight(code, …).value`
// returns the code text HTML-escaped, wrapped in `<span class="hljs-…">`. So
// the caller's dangerouslySetInnerHTML can never execute markup embedded in the
// code (e.g. `<script>` in a fence) — it renders as inert text. Keeps the #187
// strict-escape guarantee. We only ever pass hljs output (never raw code) to
// innerHTML, and `getLanguage` gating means unknown languages fall back to
// React's own text-escaping (null → plain render in the component).
import hljs from 'highlight.js/lib/core';
import './codeHighlight.css';
import bash from 'highlight.js/lib/languages/bash';
import go from 'highlight.js/lib/languages/go';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import python from 'highlight.js/lib/languages/python';
import sql from 'highlight.js/lib/languages/sql';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';

let registered = false;
function ensureRegistered(): void {
  if (registered) return;
  hljs.registerLanguage('javascript', javascript);
  hljs.registerLanguage('typescript', typescript);
  hljs.registerLanguage('python', python);
  hljs.registerLanguage('go', go);
  hljs.registerLanguage('json', json);
  hljs.registerLanguage('bash', bash);
  hljs.registerLanguage('sql', sql);
  hljs.registerLanguage('yaml', yaml);
  hljs.registerLanguage('xml', xml);
  registered = true;
}

// highlightCode returns hljs-escaped highlighted HTML for a KNOWN language, or
// null when the language is unknown/unregistered (caller then renders the raw
// string, which React escapes). Never throws on odd input (ignoreIllegals).
export function highlightCode(code: string, language: string): string | null {
  ensureRegistered();
  const lang = language.toLowerCase();
  if (!hljs.getLanguage(lang)) return null;
  return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
}
