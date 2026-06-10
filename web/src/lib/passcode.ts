// Shared client-side passcode strength validation.
//
// v2.9 #290: mirrors the backend `ValidatePasscodePlain`
// (internal/identity/passcode.go): at least 6 and at most 128 characters,
// with at least one letter, one digit, and one symbol (a real
// punctuation/symbol rune — Go unicode.IsPunct||IsSymbol, which EXCLUDES
// whitespace and control chars). The error wording matches the backend's
// distinct messages so FE↔backend stay consistent.
//
// Length is measured in code points (spread iterator) to match Go's
// utf8.RuneCountInString, so astral characters count as one.

export const PASSCODE_MIN_LEN = 6;
export const PASSCODE_MAX_LEN = 128;

// Helper line shown near every passcode input describing the rule.
export const PASSCODE_RULE_HINT =
  'At least 6 characters, including a letter, a digit, and a symbol.';

/**
 * Returns '' when `v` satisfies the passcode rule, otherwise the message for
 * the FIRST failing rule (length checks first, then letter/digit/symbol),
 * matching the backend's distinct errors.
 */
export function validatePasscodeStrength(v: string): string {
  const len = [...v].length;
  if (len < PASSCODE_MIN_LEN) return 'Passcode must be at least 6 characters';
  if (len > PASSCODE_MAX_LEN) return 'Passcode must be at most 128 characters';
  if (!/\p{L}/u.test(v)) return 'Passcode must contain a letter';
  if (!/\p{Nd}/u.test(v)) return 'Passcode must contain a digit';
  // Punctuation or symbol — matches Go unicode.IsPunct||IsSymbol; EXCLUDES
  // letters, digits, whitespace and control chars.
  if (!/[\p{P}\p{S}]/u.test(v)) return 'Passcode must contain a symbol';
  return '';
}
