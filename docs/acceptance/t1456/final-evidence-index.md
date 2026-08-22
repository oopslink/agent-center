# T1469 final evidence index

The complete binary visual evidence is attached to task `task-ae7e43b2`; it is
kept outside Git so the runtime's embedded `version.commit` can equal the final
pushed source commit without a self-referential commit hash.

Canonical source: `ac://files/01M0HRMZDX20FF5KQT4SBANGC1`, SHA256
`5e085034e927054a59c103aeac30b6217c6a8a1c5f44f20ad9212589381cf43e`,
1672×941.

Each state has three 1672×941 files: candidate, 50/50 canonical overlay, and
absolute pixel diff. `pixel-stats.json` records AE and normalized RMSE.

Structural mapping:

| Candidate | Canonical structure/state exercised |
| --- | --- |
| 01 default | Access/RAM Roles IA, summary metrics, searchable catalog, selected detail, permission summary, version history, Team Role references |
| 02 search | canonical Search RAM roles control and result filtering |
| 03 risk | canonical All risk selector and high-risk result state |
| 04 detail | selected row, permission summary, reference cards, version history |
| 05 create empty | Create new RAM Role drawer and empty form fields |
| 06 create permissions | permission selector, risk summary, scope/name/key/description fields |
| 07 toast | successful RAM Role creation toast and refreshed catalog |
| 08 edit/version | edit drawer, existing permissions, versioned-write action |
| 09 delete blocked | referenced-role deletion warning and disabled destructive action |
| 10 delete migration | migration target selector and enabled migrate/revoke path |

Whole-frame pixel mismatch is diagnostic, not a pass criterion: the canonical
is a composite design sheet containing mutually exclusive drawer, modal and
toast states simultaneously. Gate decisions must therefore use the mapped
structure/state evidence above together with each raw overlay/diff.
