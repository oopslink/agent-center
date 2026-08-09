// v2.7.1 #232: defaults an agent is created with when the operator doesn't
// override them. Kept as explicit constants (not form placeholders) so the
// create form, the stored value, and the runtime stay consistent — matches
// v2.7 #181 where the controller's default-model fallback is 'claude-opus-4-8'.
export const DEFAULT_AGENT_MODEL = 'claude-opus-4-8';
