# v1 → v2 Migration Guide

> Audience: operators of a running v1 agent-center install upgrading
> to v2.0 GA (2026-05-24). For a fresh v2 install (no v1 history),
> follow [06-deployment § 10.1](../design/implementation/06-deployment.md)
> directly — none of the steps below apply.

> v2 is a **major version** with breaking changes
> ([release notes](../release/v2.0.md)). Read those first to
> decide whether to upgrade.

---

## § 0. Why upgrade

- New Web Console (loopback SPA) replaces v1 vendor IM as the user
  entry point ([ADR-0037](../design/decisions/0037-web-console-as-main-user-ui.md))
- SecretManagement BC: centralised user-secret CRUD with at-rest
  encryption ([ADR-0026](../design/decisions/0026-user-secret-management-bc.md))
- Worker enroll v2 (bootstrap-token exchange)
  ([ADR-0023](../design/decisions/0023-worker-enroll-lightweight.md))
- AgentInstance as first-class entity
  ([ADR-0024](../design/decisions/0024-agent-instance-first-class.md))
- Conversation v2 unified model
  ([ADR-0039](../design/decisions/0039-conversation-business-model-v2-unified.md))
- Vendor IM (Feishu / Bridge BC) **removed**
  ([ADR-0031](../design/decisions/0031-v2-drop-bridge-vendor-integration.md))

If you actively depend on vendor IM, **do not upgrade** until v3 +
external IM is re-designed. v1 read-only co-existence is operator-
decided.

---

## § 1. Pre-flight checklist

```bash
# 1. Stop v1 server
sudo systemctl stop agent-center

# 2. Backup sqlite (MANDATORY)
sudo cp /var/lib/agent-center/agent-center.db \
        /var/lib/agent-center/agent-center.db.pre-v2

# 3. Backup existing config + env files (some are removed in v2)
sudo tar czf /root/agent-center-v1-config.tar.gz /etc/agent-center/

# 4. Generate v2 SecretManagement master key (if you don't have one)
#    See docs/operations/master-key.md for the full procedure.
if [[ ! -f /etc/agent-center/master.key ]]; then
  sudo install -m 0600 -o agent-center -g agent-center /dev/null \
    /etc/agent-center/master.key
  sudo bash -c \
    'head -c 32 /dev/urandom | base64 > /etc/agent-center/master.key'
  echo "BACKUP THIS KEY — see docs/operations/master-key.md § 2"
fi

# 5. Update config.yaml — add secret_management.master_key_file
sudo tee -a /etc/agent-center/config.yaml > /dev/null <<'YAML'

secret_management:
  master_key_file: "/etc/agent-center/master.key"
YAML

# 6. Drop v1 vendor config sections (notification.* / bridge.* /
#    notification.default_channel) — see 04-configuration.md § 7.4-7.5
#    These cause unknown-key errors in v2.
sudo $EDITOR /etc/agent-center/config.yaml
```

### Critical: master.key backup

The master key encrypts every UserSecret at rest. **If you lose it,
every secret is unrecoverable** — agents needing those credentials
must be reconfigured from scratch. Back up the key off-machine before
the first v2 secret is created. Procedure: [docs/operations/master-key.md § 2](../operations/master-key.md#-2-backup).

---

## § 2. Run the migration

### 2.1 Dry-run

```bash
sudo -u agent-center /usr/local/bin/agent-center migrate v1-to-v2 \
    --config=/etc/agent-center/config.yaml --dry-run
```

Expected output (numbers vary by install):

```
current schema version: 6
target  schema version: 25
bridge rows to archive:
  feishu_delivery_ledger:    427
  bridge_subscription_cursors: 1
dry-run: no changes applied
```

Read the numbers:

- **current schema version** ≤ 25 — the tool will run the missing
  migrations.
- **bridge rows** — these will be exported to a JSON archive (§ 4
  below).
- If `current == 25` already, output is `already at v2; no action`
  and the upgrade is a no-op.

### 2.2 Apply

```bash
sudo -u agent-center /usr/local/bin/agent-center migrate v1-to-v2 \
    --config=/etc/agent-center/config.yaml --apply
```

Expected output:

```
current schema version: 6
target  schema version: 25
bridge rows to archive: ...
bridge archive written: /var/lib/agent-center/migration-archive/bridge-archive-20260524T093017Z.json
migration applied; new schema version: 25
```

The tool is **idempotent** — running `--apply` a second time exits 0
with `already at v2`.

---

## § 3. Verify v2 server boots

```bash
# 1. Install the v2 binary (pre-built from the v2.0 release)
sudo install -m 0755 agent-center-darwin-arm64-<sha> /usr/local/bin/agent-center

# 2. Replace systemd unit (v2 dropped the feishu EnvironmentFile line)
sudo install -m 0644 contrib/agent-center.service \
    /etc/systemd/system/agent-center.service
sudo systemctl daemon-reload

# 3. Start
sudo systemctl start agent-center
sudo journalctl -u agent-center -f
# look for:
#   agent-center server: db=... listen=:7000 web=127.0.0.1:7100 (escalator running)

# 4. Smoke test the Web Console (loopback only per ADR-0037; SSH-tunnel
#    if accessing from another machine)
curl -sS http://127.0.0.1:7100/api/health
# expect: {"ok":true,...}

# 5. CLI version check
sudo -u agent-center /usr/local/bin/agent-center version
# expect: v2.0.0
```

---

## § 4. Bridge archive — what it is, why we keep it

`migrate v1-to-v2 --apply` snapshots the rows of the v1 Bridge BC
tables (`feishu_delivery_ledger`, `bridge_subscription_cursors`)
BEFORE migration 0025 drops them. The snapshot goes to
`<sqlite-dir>/migration-archive/bridge-archive-<UTC-ts>.json` with
this shape:

```json
{
  "exported_at": "2026-05-24T09:30:17Z",
  "sqlite_path": "/var/lib/agent-center/agent-center.db",
  "schema_version_before": 6,
  "bridge": {
    "feishu_delivery_ledger": [
      { "id": "led-1", "message_id": "m-1", "conversation_id": "c-1",
        "channel": "feishu", "status": "delivered", ... }
    ],
    "bridge_subscription_cursors": [
      { "subscriber": "feishu_outbound", "last_event_id": "01ABC",
        "updated_at": "..." }
    ]
  }
}
```

When you need it:
- Audit: "what did v1 deliver to vendor X at time Y?" — search the
  JSON.
- Forensic: vendor disputed a delivery; ledger row proves what was
  attempted.

When you can throw it away: when you're confident the v1 vendor
delivery audit is no longer load-bearing (e.g. 90 days post-
migration with no vendor escalations).

---

## § 5. Rollback

The migration tool does NOT support automated rollback. Procedure:

```bash
sudo systemctl stop agent-center
sudo cp /var/lib/agent-center/agent-center.db.pre-v2 \
        /var/lib/agent-center/agent-center.db
# Reinstall v1 binary + restore v1 config from your tarball:
sudo tar xzf /root/agent-center-v1-config.tar.gz -C /
sudo install -m 0755 /path/to/agent-center-v1-<sha> /usr/local/bin/agent-center
sudo install -m 0644 /path/to/v1-agent-center.service \
    /etc/systemd/system/agent-center.service
sudo systemctl daemon-reload
sudo systemctl start agent-center
```

This relies entirely on the pre-flight backup from § 1. **Test your
backup** before invoking `--apply` (e.g. on a snapshot VM).

The migration `--down` direction does exist in the schema migrator
(`migrate up --target=N`) but down-migrations on real production data
have not been v2-GA tested for this v1→v2 path. Treat them as a
last-resort emergency tool, not a routine rollback.

---

## § 6. FAQ / common pitfalls

**Q. The dry-run reports `current schema version: 5` (or another
unexpected value).**
A. Your v1 install is older than the v2-supported migration baseline
(0006). Apply v1's own migrations to bring it to 6 first
(`agent-center migrate up` on the v1 binary), then re-run the dry-
run.

**Q. Apply says "config: unknown_section: notification" or similar.**
A. You didn't strip the v1 vendor config sections per § 1 step 6. v2
rejects unknown YAML keys to catch typos
([04-configuration § 4](../design/implementation/04-configuration.md#-4-验证--启动失败语义)).

**Q. After upgrade the v2 server logs "load master key: missing
file".**
A. `secret_management.master_key_file` points at a missing path. Re-
do § 1 step 4.

**Q. Web Console is unreachable on `127.0.0.1:7100`.**
A. v2 binds loopback only ([ADR-0037](../design/decisions/0037-web-console-as-main-user-ui.md)).
Use SSH tunnel: `ssh -L 7100:127.0.0.1:7100 vps` then
`http://127.0.0.1:7100/` in your local browser.

**Q. Idempotent re-run wrote a second archive file.**
A. It shouldn't — `--apply` on a v2 DB skips both archive and
migration. If you see a second archive, the tool detected the bridge
tables still present (meaning migration 0025 hasn't been applied). File
a bug.

---

## § 7. References

- [phase-12-audits/s12-migration-tool-audit.md](../plans/phase-12-audits/s12-migration-tool-audit.md)
  — design + test plan
- [docs/operations/master-key.md](../operations/master-key.md)
  — master-key operational procedures
- [docs/release/v2.0.md](../release/v2.0.md)
  — what changed in v2
- [docs/design/implementation/06-deployment.md](../design/implementation/06-deployment.md)
  — v2 deployment topology
