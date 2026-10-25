// 리뷰 화면이 쓰는 표시 문자열. 자리를 채우는 말이다.
//
// 「무엇이 일어났나」를 말하는 카테고리 이름·개입 문구는 서버가 만든다(review.go).

import type { Messages } from '@/i18n';
import type { Locale } from '@/i18n/locale';
import type { GameResult } from '@/protocol/review';

/**
 * 사람 기준 결과. 끝나지 않은 판에는 값이 오지 않으므로 `ongoing` 을 쓴다.
 *
 * `ongoing` 은 「中断」과 다르다. 그쪽은 끝난 것으로 닫힌 판이다.
 */
export function resultJa(result: GameResult | undefined, t: Messages): string {
  const r = t.review.result;
  return result ? (r[result] ?? r.ongoing) : r.ongoing;
}

/**
 * 날짜. 화면 언어의 로케일로 고정한다.
 *
 * 브라우저 로케일을 따르면 같은 화면이 사람마다 다른 말로 나온다.
 */
const DATE_FORMATS: Record<Locale, Intl.DateTimeFormat> = {
  ja: new Intl.DateTimeFormat('ja-JP', { month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit' }),
  ko: new Intl.DateTimeFormat('ko-KR', { month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit' }),
};

export function dateJa(iso: string, locale: Locale): string {
  const at = new Date(iso);
  // 읽을 수 없는 값으로 「Invalid Date」를 화면에 내보내지 않는다.
  return Number.isNaN(at.getTime()) ? '' : DATE_FORMATS[locale].format(at);
}
