-- Reverse 0031.

DROP INDEX IF EXISTS idx_admin_tokens_worker_id;

ALTER TABLE admin_tokens DROP COLUMN plaintext_nonce;
ALTER TABLE admin_tokens DROP COLUMN plaintext_ciphertext;
ALTER TABLE admin_tokens DROP COLUMN worker_id;
