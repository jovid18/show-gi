// 경로 문자열은 여기만 안다. **트리와 갈라 둔다** — 링크를 그리는 쪽은 상수만 필요하고,
// 상수가 한 곳에 있으면 「어떤 주소가 있나」를 파일 하나로 읽을 수 있다.

export const ROUTE_GAME = '/';
export const ROUTE_REVIEWS = '/reviews';

/** 판 하나. 주소에 id가 들어가는 유일한 자리다. */
export const routeReview = (id: number): string => `${ROUTE_REVIEWS}/${id}`;

/** `/reviews/:id` 의 첫 조각. `parseRoute` 와 위 상수가 같은 낱말을 쓰게 묶어 둔다. */
export const REVIEWS_SEGMENT = 'reviews';
