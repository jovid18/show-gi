/**
 * 화면 언어. 일본어가 기본이고 한국어를 고를 수 있다.
 *
 * 처음에는 브라우저 언어를 따른다. 사람이 고르면 `localStorage` 에 남겨 다음 방문에도 쓴다.
 * 서버가 만드는 문장(개입 문구·카테고리 이름·棋譜)은 언어와 무관하게 일본어로 온다.
 */
export type Locale = 'ja' | 'ko';

export const LOCALES: readonly Locale[] = ['ja', 'ko'];

const STORAGE_KEY = 'showgi.locale';

/** 저장된 값이 우선이고, 없으면 브라우저 언어가 `ko` 로 시작할 때만 한국어다. */
export function localeFrom(saved: string | null, browser: readonly string[]): Locale {
  if (saved === 'ja' || saved === 'ko') return saved;
  return browser[0]?.toLowerCase().startsWith('ko') ? 'ko' : 'ja';
}

function initial(): Locale {
  let saved: string | null = null;
  try {
    saved = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    // 읽지 못하면 브라우저 언어로 정한다.
  }
  return localeFrom(saved, typeof navigator === 'undefined' ? [] : navigator.languages);
}

let locale: Locale = typeof window === 'undefined' ? 'ja' : initial();

// `index.html` 은 `lang="ja"` 로 나간다. 한국어로 시작하면 읽기 도구가 일본어로 읽지 않게 고친다.
if (typeof document !== 'undefined') document.documentElement.lang = locale;

const listeners = new Set<() => void>();

/** `useSyncExternalStore` 가 읽으므로 값이 그대로면 알리지 않는다. */
export function setLocale(next: Locale): void {
  if (locale === next) return;
  locale = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // 저장에 실패해도 지금 화면에는 적용한다.
  }
  document.documentElement.lang = next;
  for (const notify of listeners) notify();
}

export function subscribeLocale(notify: () => void): () => void {
  listeners.add(notify);
  return () => {
    listeners.delete(notify);
  };
}

export function getLocale(): Locale {
  return locale;
}
