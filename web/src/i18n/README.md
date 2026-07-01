# i18n — translation guide (v2.25)

The console supports **中文 (`zh`)** and **English (`en`)**, switchable from
**System → Settings → Language** and persisted per-device. This document is the
contract every translation stream must follow so the 5 module streams can work
in parallel without merge conflicts.

## Stack

Standard community stack, initialised once in [`index.ts`](./index.ts):

- [`i18next`](https://www.i18next.com/)
- [`react-i18next`](https://react.i18next.com/) — `useTranslation()` hook
- [`i18next-browser-languagedetector`](https://github.com/i18next/i18next-browser-languageDetector)

Persistence lives in [`lang.ts`](./lang.ts) (mirrors `theme.ts`): localStorage
key **`ac.lang`**, value `'zh' | 'en'`, with a `navigator.language` fallback. The
detector only *reads* that key — `lang.ts` is the sole writer (via `writeLang`).
`applyInitialLang()` runs in `main.tsx` before React mounts, so there is no flash
of fallback copy on reload.

## Namespaces (one per module — your isolation boundary)

```
common    shared shell/nav/buttons/empty-states + entry pages  (owned by F0)
chat      Conversations module (channels, DMs, composer, …)
work      Workspace module (projects, issues, tasks, plans, repos)
members   Members module (humans, agents)
admin     System module (environment, settings, version, org settings)
insights  Insights module
```

Each namespace is two JSON files: `locales/en/<ns>.json` and
`locales/zh/<ns>.json`. **All 12 are pre-registered in `index.ts`.**

## Rules for module streams

1. **Edit only your namespace's two JSON files + your module's components.**
   Do **not** touch `index.ts`, `lang.ts`, or another stream's JSON. `index.ts`
   is owned by F0 as the single registration point — that is what makes parallel
   work conflict-free.
2. In components: `const { t } = useTranslation('<your-namespace>')`.
3. **Key naming**: dot-separated, semantic, scoped to a feature — e.g.
   `chat.composer.send`, `work.issues.empty`. Keep the same key tree in `en` and
   `zh`.
4. **No hard-coded user-visible English** — that includes button text, empty/
   error states, `placeholder`, `aria-label`, `title`, and toasts.
5. **Interpolation**: `t('chat.unread', { count })` with `"unread": "{{count}} unread"`.
   For plurals use i18next's `_one` / `_other` suffix keys + the `count` option.
6. **Don't translate identity/keys.** Anything used as a React `key`, a
   `data-testid`, a localStorage key, or a routing/active-state discriminator
   must stay a stable literal. Translate the *display* string at render time,
   keyed off a stable id (see `moduleLabel(id)` in `AppLayout.tsx` for the
   pattern — the module `id` is the key, the label is localised).

## Reference implementation (F0)

F0 converted the `common` namespace + the shell/entry slices as the worked
example to copy:

- `AppLayout.tsx` — top-level module rail labels + theme control, via
  `t('nav.<id>')` / `t('theme.*')` rendered from stable ids.
- `pages/Settings.tsx` + `components/LanguagePanel.tsx` — the Language switch.
- `pages/Version.tsx`, `pages/NotFound.tsx` — entry/misc pages.

## Tests

`src/test/setup.ts` imports `../i18n`, so i18next is initialised for the whole
vitest run. In jsdom `navigator.language` is `en-US` and no `ac.lang` is stored,
so `t()` resolves to **English** — existing assertions on English copy keep
passing. To test the zh rendering, call `i18n.changeLanguage('zh')` in the test.
