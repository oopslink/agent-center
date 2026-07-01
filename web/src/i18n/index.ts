// i18n bootstrap — v2.25 F0. Standard community stack: i18next +
// react-i18next + i18next-browser-languagedetector.
//
// Design notes:
//   - ALL six namespaces for BOTH languages are registered up-front as static
//     resources (no async backend / HTTP load). This is the seam that lets the
//     5 parallel translation streams work without conflict: each stream edits
//     ONLY its own namespace json (e.g. chat.json), never this file. F0 owns
//     index.ts as the single registration point.
//   - Persistence is owned by lang.ts (localStorage key `ac.lang`, written via
//     writeLang). The detector READS that same key, so the two never disagree;
//     we set caches:[] so the detector never writes (lang.ts is the sole writer).
//   - escapeValue:false — React already escapes; i18next must not double-escape.
//   - useSuspense:false — resources are synchronous, and tests render without a
//     Suspense boundary; this keeps first render synchronous.
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import enCommon from './locales/en/common.json';
import enChat from './locales/en/chat.json';
import enWork from './locales/en/work.json';
import enMembers from './locales/en/members.json';
import enAdmin from './locales/en/admin.json';
import enInsights from './locales/en/insights.json';
import zhCommon from './locales/zh/common.json';
import zhChat from './locales/zh/chat.json';
import zhWork from './locales/zh/work.json';
import zhMembers from './locales/zh/members.json';
import zhAdmin from './locales/zh/admin.json';
import zhInsights from './locales/zh/insights.json';

import { SUPPORTED_LANGS, DEFAULT_LANG } from './lang';

// Single source of truth for the namespace list (registration + README contract).
export const NAMESPACES = ['common', 'chat', 'work', 'members', 'admin', 'insights'] as const;
export type Namespace = (typeof NAMESPACES)[number];

const resources = {
  en: {
    common: enCommon,
    chat: enChat,
    work: enWork,
    members: enMembers,
    admin: enAdmin,
    insights: enInsights,
  },
  zh: {
    common: zhCommon,
    chat: zhChat,
    work: zhWork,
    members: zhMembers,
    admin: zhAdmin,
    insights: zhInsights,
  },
} as const;

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    ns: NAMESPACES as unknown as string[],
    defaultNS: 'common',
    fallbackLng: DEFAULT_LANG,
    supportedLngs: SUPPORTED_LANGS as unknown as string[],
    // 'zh-CN'/'en-US' → 'zh'/'en' rather than missing-language fallback.
    load: 'languageOnly',
    nonExplicitSupportedLngs: true,
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'ac.lang',
      caches: [], // lang.ts owns writes; the detector must not also write.
    },
    interpolation: { escapeValue: false },
    react: { useSuspense: false },
  });

export default i18n;
