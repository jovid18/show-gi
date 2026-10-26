import { useLocale, useT } from './index';
import { LOCALES, setLocale, type Locale } from './locale';

/** 언어 이름은 언제나 그 언어로 쓴다. 읽지 못하는 말로 적힌 자기 언어는 찾을 수 없다. */
const NATIVE: Record<Locale, string> = {
  ja: '日本語',
  ko: '한국어',
};

/** 헤더 오른쪽 끝의 언어 전환. 고른 값은 다음 방문에도 남는다(`locale.ts`). */
export function LocaleSwitch() {
  const t = useT();
  const locale = useLocale();
  return (
    <select
      className="app-locale"
      aria-label={t.common.language}
      value={locale}
      onChange={(e) => setLocale(e.target.value as Locale)}
    >
      {LOCALES.map((l) => (
        <option key={l} value={l} lang={l}>
          {NATIVE[l]}
        </option>
      ))}
    </select>
  );
}
