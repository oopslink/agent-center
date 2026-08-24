# T1498 RAM Role Classification Contract

RAM Role current state is classified by `authorization_roles.kind` and
`authorization_roles.visibility`.

| Classification | Contract |
|---|---|
| reusable system | `kind='system'`, `visibility='reusable'`, `org_id=''`; release seed/migration maintained; admins cannot edit, version, or revoke |
| reusable custom | `kind='custom'`, `visibility='reusable'`, org-owned; admin-managed through RAM Role API |
| managed internal | `kind='managed'`, `visibility='internal'`, org-owned; created only by managed Access flows, hidden from RAM Role catalog and Team Role RAM Role resolution |

Reusable RAM Roles must not use the reserved `Access grant...` prefix. Historical
direct-grant rows using that prefix are migrated to managed/internal and renamed
to `Managed direct grant...`.

## Built-In System RAM Roles

| Stable key | Scenario | Least-privilege closure | Reuse entry points | Risk |
|---|---|---|---|---|
| `team-basic` | Team Roles that need read-only access to team configuration and team memory. | `team.read`, `team.memory.read` | `team.role.ram_role_keys`; `team.role.ram_roles.mapping` | low |
| `team-contributor` | Team Roles that participate in team work and may propose, but not approve, memory changes. | `team.read`, `team.write`, `team.memory.read`, `team.memory.propose` | `team.role.ram_role_keys`; `team.role.ram_roles.mapping` | medium |
| `team-curator` | Team Roles that curate promoted team memory while retaining contributor capabilities. | `team.read`, `team.write`, `team.memory.read`, `team.memory.propose`, `team.memory.review` | `team.role.ram_role_keys`; `team.role.ram_roles.mapping` | high |
