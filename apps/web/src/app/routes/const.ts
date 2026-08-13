// 경로 문자열은 여기만 안다. **트리와 갈라 둔다** — 링크를 그리는 쪽은 상수만 필요하고,
// 상수가 한 곳에 있으면 「어떤 주소가 있나」를 파일 하나로 읽을 수 있다.

export const ROUTE_GAME = '/';
export const ROUTE_REVIEWS = '/reviews';

/** 마이페이지. **판이 아니라 사람 하나**라 id가 없다. */
export const ROUTE_ME = '/me';

/**
 * 판 하나. 주소에 id가 들어가는 유일한 자리다.
 *
 * `ply` 를 주면 **그 手数에서 열린다**. 총평이 「이 국면을 다시 봐라」로 짚은 자리가
 * 링크가 되려면 手数도 주소에 있어야 한다 — 화면 안의 상태로 두면 새로고침에 사라진다.
 */
export const routeReview = (id: number, ply?: number): string =>
  ply === undefined ? `${ROUTE_REVIEWS}/${id}` : `${ROUTE_REVIEWS}/${id}/${ply}`;

/**
 * 그 판의 퀴즈. **되짚기 아래에 둔다** — 문항이 그 판에서 나온 것이고, 주소가 그 사실을
 * 말해야 뒤로 가기가 그 판으로 돌아간다.
 */
export const routeQuiz = (id: number): string => `${routeReview(id)}/${QUIZ_SEGMENT}`;

/** `/reviews/:id` 의 첫 조각. `parseRoute` 와 위 상수가 같은 낱말을 쓰게 묶어 둔다. */
export const REVIEWS_SEGMENT = 'reviews';

/** `/reviews/:id/quiz` 의 마지막 조각. */
export const QUIZ_SEGMENT = 'quiz';

/** `/me` 의 첫 조각. */
export const ME_SEGMENT = 'me';
