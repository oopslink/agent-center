// v2.7.1 #232: defaults an agent is created with when the operator doesn't
// override them. Kept as explicit constants (not form placeholders) so the
// create form, the stored value, and the runtime stay consistent — matches
// v2.7 #181 where the controller's default-model fallback is 'claude-opus-4-8'.
export const DEFAULT_AGENT_MODEL = 'claude-opus-4-8';

// Preset model ids surfaced as <datalist> suggestions on the main "Model" field
// of the create + edit forms. These are HINTS only — the field stays free text
// (an <input list>, not a <select>) so operators can still type any model the
// backend accepts. DEFAULT_AGENT_MODEL is intentionally the first entry.
export const KNOWN_MODELS = [
  'claude-opus-4-8',
  'claude-sonnet-5',
  'claude-haiku-4-5-20251001',
  'claude-fable-5',
];
