import catalog from './messages.json';
export type Locale = 'en' | 'ru' | 'uk';
export type LocalePreference = Locale | 'auto';
export type MessageKey = keyof typeof catalog;
let current: Locale = 'en';
const listeners = new Set<() => void>();
export const getLocale = () => current;
export const subscribeLocale = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; };
export function resolveLocale(value: string, languages: readonly string[] = []): Locale {
  const choices = value === 'auto' ? languages : [value];
  for (const candidate of choices) {
    const lang = candidate.toLowerCase().split(/[-_]/)[0];
    if (lang === 'ua') return 'uk';
    if (lang === 'en' || lang === 'ru' || lang === 'uk') return lang;
  }
  return 'en';
}
export function setLocale(value: Locale) {
  if (typeof document !== 'undefined') document.documentElement.lang = value;
  if (value === current) return;
  current = value;
  listeners.forEach(fn => fn());
}
/** Translate only application-owned messages; never feed source titles through this. */
export function t(key: MessageKey, values: Record<string, string | number> = {}): string {
  return translate(key, current, values);
}
export function translate(key: string, locale: Locale, values: Record<string, string | number> = {}): string {
  const row = (catalog as Record<string, {ru:string; uk:string}>)[key];
  const text = locale === 'en' ? key : row?.[locale] ?? key;
  return text.replace(/\{(\w+)\}/g, (match, name: string) => values[name] === undefined ? match : String(values[name]));
}
/** Built-in dynamic form/status labels may include third-party fallback text. */
export const label = (text: string) => translate(text, current);
export function plural(count: number, forms: Partial<Record<Intl.LDMLPluralRule, string>> & {other:string}, locale = current) {
  return (forms[new Intl.PluralRules(locale).select(count)] ?? forms.other).replace(/\{count\}/g, new Intl.NumberFormat(locale).format(count));
}
