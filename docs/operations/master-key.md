# Master Key — operational procedures

> Audience: operators of an agent-center install. The master key
> encrypts every UserSecret at rest. If you lose it, the secrets
> are unrecoverable. This doc covers generation / backup / rotation
> / multi-machine / disaster recovery.
>
> Design rationale: [ADR-0026 § 4](../design/decisions/0026-user-secret-management-bc.md).

---

## § 0. What it is + why it matters

- 32-byte AES-256 key, stored as base64 in a single file (mode 0600).
- Loaded once at center process start; lives in memory only thereafter.
- Used to AES-GCM encrypt the `value` column of `user_secrets`
  table rows.
- Each plaintext secret (MCP API key, cloud credential, ...) is
  encrypted before persistence; the plaintext is NEVER stored on
  disk in any form (per ADR-0026 § 5 plaintext-never-echo).

**Loss profile**:

| Lost | Effect |
|---|---|
| In-memory copy (process crash, server restart) | None — reloaded from disk at next start |
| Disk copy (file deleted) but in-memory still running | Server keeps running until next restart; THEN all secrets unreadable |
| Both disk + in-memory + no backup | All UserSecrets are dead. Agents that need those credentials must be reconfigured from scratch. See § 5. |

---

## § 1. First-time generation

```bash
sudo install -m 0600 -o agent-center -g agent-center /dev/null \
    /etc/agent-center/master.key
sudo bash -c \
    'head -c 32 /dev/urandom | base64 > /etc/agent-center/master.key'
```

Verify:

```bash
sudo stat -f '%Sp' /etc/agent-center/master.key
# expect: -rw-------
sudo wc -c < /etc/agent-center/master.key
# expect: 45  (32 bytes base64-encoded = 44 chars + newline)
```

Wire into config (one-time):

```yaml
# /etc/agent-center/config.yaml
secret_management:
  master_key_file: "/etc/agent-center/master.key"
```

Restart the server after editing the config.

---

## § 2. Backup

**Back up immediately**, before any secret is created. The first
secret you create commits to this specific key forever (until you
rotate per § 3 which destroys all secrets).

Acceptable backup locations:

| Method | Notes |
|---|---|
| Password manager (1Password / Bitwarden / KeePass) | Best; encrypted at rest; easy multi-operator share |
| Hardware token (YubiKey / OnlyKey static slot) | Best for solo operator |
| Sealed envelope in a physical safe | Old-school but works |
| Cloud KMS (envelope-encrypted) | v3 candidate — see § 4 |

**Forbidden backup locations**:

- `git` (any repo, even private — branches leak, snapshots are
  immutable, key search engines comb them constantly)
- Plain email / Slack / IM
- Unencrypted cloud drive (Dropbox / iCloud / Google Drive without
  client-side encryption)
- Shared filesystems (NFS, SMB) without ACL audit

Record who has the backup, when it was made, and where:

```
# /root/master-key-backup.log (root-readable only)
2026-05-24  initial gen + backup to 1Password vault "Ops/agent-center"
            owner: hayang@example.com
```

---

## § 3. Rotation

**v2 does NOT support in-place rotation.** The reason: UserSecrets
are encrypted at rest with the master key; rotating the key without
re-encrypting every secret would render them all unreadable. v2
doesn't implement the envelope-rotation pattern (defer to v3).

If you MUST rotate (e.g. key compromise):

```bash
# 1. Stop the server
sudo systemctl stop agent-center

# 2. Mark every UserSecret revoked (so v2 won't try to read with
#    the old key after rotation; revoked rows are terminal)
sqlite3 /var/lib/agent-center/agent-center.db \
    "UPDATE user_secrets SET state='revoked', revoked_at=datetime('now'),
     ended_reason='master_key_rotation',
     ended_message='operator rotated master key 2026-05-24';"

# 3. Generate a new key (overwrite the file or rename + new)
sudo bash -c \
    'head -c 32 /dev/urandom | base64 > /etc/agent-center/master.key'

# 4. Back up the new key per § 2.

# 5. Restart
sudo systemctl start agent-center

# 6. Recreate every secret via `agent-center secret create ...`.
#    Agents that referenced revoked secrets via `secret:<name>` will
#    fail at next dispatch until you create + re-reference the new
#    secret with the same name.
```

This is destructive — only do it if the old key is compromised. For
routine "rotate the key annually" hygiene, v3 envelope rotation is
the right answer; v2 has no good story.

---

## § 4. Multi-machine sync

**v2 is single-node by design.** There is no multi-machine sync
scenario. Each agent-center install has its own master key + its
own UserSecrets.

If you spin up a second machine (failover / migration):

- The new machine generates its OWN master key.
- UserSecrets do NOT transfer (the old machine's encrypted bytes
  can't be decrypted by the new machine's key).
- Recreate secrets on the new machine.

v3 candidate: KMS adapter (AWS KMS / GCP KMS / Vault) that hosts the
master key remotely; multi-machine installs all dial the same KMS.
Not in v2 scope.

---

## § 5. Disaster recovery — lost master.key, no backup

If you've lost the master key + don't have a backup:

```bash
# 1. Stop the server
sudo systemctl stop agent-center

# 2. Delete all user_secrets rows (they're unrecoverable)
sqlite3 /var/lib/agent-center/agent-center.db \
    "DELETE FROM user_secrets;"

# 3. Generate a fresh master key per § 1
# 4. Back up per § 2
# 5. Restart
sudo systemctl start agent-center

# 6. Reconfigure every agent that depended on a UserSecret:
#    - Use `agent-center secret create ...` to create the new
#      version (likely same name; new content from upstream)
#    - Agents using `secret:<name>` syntax will automatically pick
#      up the new value at next dispatch
```

Operational cost: depends on how many secrets your agents use.
Plan for a half-day-to-day-long re-create cycle for any non-trivial
install.

**Prevention is cheaper than recovery** — back up per § 2 before the
first secret.

---

## § 6. Audit log of key events

Every UserSecret CRUD emits an `observability` event (visible in
`events` table; queryable via `agent-center inspect`). The event
payload includes `id`, `name`, `kind`, `actor`, but never the
plaintext value. Use this to audit who created / revoked which
secret.

Master-key events (load success / load failure) only appear in
the server's stderr log (journald via systemd). Tail with:

```bash
sudo journalctl -u agent-center | grep -i 'master.key\|secret'
```

---

## § 7. References

- [ADR-0026 § 4-5](../design/decisions/0026-user-secret-management-bc.md)
  — design rationale (at-rest encryption + plaintext-never-echo)
- [ADR-0027](../design/decisions/0027-mcp-per-agent-injection.md)
  — how secrets feed MCP at agent runtime
- [docs/migration/v1-to-v2.md § 1](../migration/v1-to-v2.md#-1-pre-flight-checklist)
  — first-time master-key generation during upgrade
- [docs/design/implementation/04-configuration.md § 7.9](../design/implementation/04-configuration.md)
  — `secret_management.*` config keys
