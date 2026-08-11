// 리뷰 화면이 쓰는 표시 문자열.
//
// **여기 있는 것은 코드가 아니라 자리다.** 카테고리 이름·개입 문구처럼 「무엇이 일어났나」를
// 말하는 말은 서버가 만든다(review.go). 이 파일에 있는 것은 그 바깥의 것들 — 결과의 이름과
// 날짜 형식이라, 서버가 보낸 사실을 다시 쓰는 것이 아니다.

import type { GameResult } from './protocol';

/** 사람 기준 결과. 끝나지 않은 판에는 값이 안 오므로 `ONGOING`을 쓴다. */
export const RESULT_JA: Record<GameResult, string> = {
  win: '勝ち',
  loss: '負け',
  draw: '千日手',
  abandoned: '中断',
};

/** 아직 끝나지 않은 판. 「中断」과 다르다 — 그쪽은 끝난 것으로 닫힌 판이다. */
export const ONGOING_JA = '対局中';

export function resultJa(result: GameResult | undefined): string {
  return result ? (RESULT_JA[result] ?? ONGOING_JA) : ONGOING_JA;
}

/**
 * 날짜. **`ja-JP`를 못 박는다** — 브라우저 로케일을 따르면 같은 화면이 사람마다 다른
 * 언어로 나오고, 이 앱의 화면은 전부 일본어다.
 */
const DATE_JA = new Intl.DateTimeFormat('ja-JP', {
  month: 'long',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
});

export function dateJa(iso: string): string {
  const at = new Date(iso);
  // 못 읽는 값으로 「Invalid Date」를 화면에 내보내지 않는다.
  return Number.isNaN(at.getTime()) ? '' : DATE_JA.format(at);
}
