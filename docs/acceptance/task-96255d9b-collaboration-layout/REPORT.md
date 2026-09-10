# Collaboration layout responsive smoke

Task: `task-96255d9b`

Verdict: **PASS**

The formal `/organizations/{org}/insights/collaboration` route was exercised with the production React application against an authenticated local Agent Center instance. The collaboration query was replaced in-page with a deterministic graph fixture so layout measurements did not depend on mutable local projection data; all page shell, routing, filters, graph rendering, timeline, and evidence code remained production code.

## Viewport measurements

| Viewport | Toolbar (W×H) | Graph panel (W×H) | Visible SVG (W×H) | Timeline (W×H) | document scroll/client width | SVG clipped by panel |
|---|---:|---:|---:|---:|---:|---:|
| 1440×900 | 1095×76 | 795×578 | 769×390 | 288×578 | 1440/1440 | no |
| 1280×720 | 935×76 | 635×398 | 609×192 | 288×398 | 1280/1280 | no |
| 1024×768 | 679×142 | 475×380 | 449×192 | 192×380 | 1024/1024 | no |

All page and graph containers reported `overflow: hidden`; the graph and timeline terminate inside the 24 px bottom content inset at each viewport. At 1024 px the primary filters wrap to two compact rows while advanced controls remain in a popover.

## Interaction smoke

- Opened **More filters** at 1024×768: popover rect `x=228, y=227, w=672, h=150`, viewport width remained `1024/1024`.
- Selected Relationship = Assign: URL retained `relation_type=assign`, the active badge appeared, and the panel remained user-collapsible.
- Zoom In changed SVG viewBox to `323.4 134.95 213.2 250.1`; Fit remained operable.
- Switched to Task impact: active tab and URL `view=impact` updated while the graph remained rendered.
- Opened an edge evidence drawer: rect `x=512, y=0, w=512, h=768`; evidence `pm.task.assigned` rendered and document width remained `1024/1024`.

## Screenshots

- `evidence/1440x900.png`
- `evidence/1280x720.png`
- `evidence/1024x768.png`
- `evidence/1024x768-more-filters.png`
- `evidence/1024x768-evidence.png`

