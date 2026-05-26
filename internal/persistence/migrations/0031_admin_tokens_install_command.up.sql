-- 0031: v2.5-B2 (#agent-center:257286ce) — let the Web Console
-- re-display an unburned enroll token's install command after the
-- Add Worker Modal closes.
--
-- The mint flow only returns plaintext once; once gone, the user
-- can't recover the `--token=...` they need to paste into
-- `./install worker`. v2.5 decouples "add worker" from "install
-- worker" — the worker row is created at mint time and the user
-- may run the install later — so the show-install-command endpoint
-- (#50) needs the plaintext back.
--
-- Three new columns on admin_tokens, all nullable, only populated
-- for enroll tokens:
--
--   worker_id              — owner-binding: which Worker AR this
--                            token was minted for. Indexed for fast
--                            "active enroll token for worker X"
--                            lookup.
--   plaintext_ciphertext   — AES-GCM ciphertext of the `acat_…`
--                            bearer, encrypted with the same
--                            master_key UserSecret BC uses. NULL
--                            for long-term tokens AND for any
--                            enroll token that has been consumed
--                            (cleared by ConsumeEnrollToken so a
--                            burned token can never be re-shown).
--   plaintext_nonce        — paired 12-byte GCM nonce. NULL iff
--                            plaintext_ciphertext IS NULL.
--
-- Schema invariant: plaintext_ciphertext IS NULL ⇒ the Web Console
-- show-install-command endpoint 401s for this row. Long-term
-- bearers keep all three columns NULL and stay hash-only.
--
-- Why encrypted-at-rest instead of raw plaintext: matches the
-- v2.4-era plaintext-never-at-rest invariant (ADR-0026,
-- UserSecret BC). Backup leaks / readable DB files no longer
-- surface bearer plaintext without also losing master_key — same
-- trust-boundary UserSecret already enforces.

ALTER TABLE admin_tokens ADD COLUMN worker_id TEXT;
ALTER TABLE admin_tokens ADD COLUMN plaintext_ciphertext BLOB;
ALTER TABLE admin_tokens ADD COLUMN plaintext_nonce BLOB;

-- Lookup index for show-install-command and re-mint flows
-- (workforce.worker -> active enroll token). Partial: only enroll
-- rows enter the index.
CREATE INDEX idx_admin_tokens_worker_id
  ON admin_tokens (worker_id)
  WHERE is_enroll = 1 AND worker_id IS NOT NULL;
