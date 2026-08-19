// 경로 문자열은 여기만 안다. **트리와 갈라 둔다** — 링크를 그리는 쪽은 상수만 필요하고,
// 상수가 한 곳에 있으면 「어떤 주소가 있나」를 파일 하나로 읽을 수 있다.

export const ROUTE_GAME = '/';
export const ROUTE_REVIEWS = '/reviews';

/** 마이페이지. **판이 아니라 사람 하나**라 id가 없다. */
export const ROUTE_ME = '/me';

/**
 * 검토. **판을 주소에 담는 유일한 화면이다.**
 *
 * 手合割 하나와 지금까지 둔 수순이 쿼리에 실린다(`?h=nimaiochi&m=7g7f,3c3d`). 새로고침·
 * 뒤로 가기·링크 공유가 그것으로 살아나고, 그러지 않으면 20手를 걸어 들어간 국면이
 * 새로고침 한 번에 0手目로 돌아간다.
 *
 * **국면(SFEN)이 아니라 수순이다.** 서버가 받는 것도 그것이라(explore.go) 주소와 요청이
 * 같은 말을 하고, 남이 준 링크가 「있을 수 없는 판」을 열 수가 없다.
 */
export const ROUTE_EXPLORE = '/explore';

/** `/explore` 의 첫 조각. */
export const EXPLORE_SEGMENT = 'explore';

/** 手合割을 싣는 쿼리 이름. 없거나 비면 平手다. */
export const EXPLORE_PARAM_HANDICAP = 'h';

/** 수순을 싣는 쿼리 이름. `,` 로 이어 붙인다. */
export const EXPLORE_PARAM_MOVES = 'm';

/**
 * 검토 화면의 주소.
 *
 * **`,` 와 `*` 를 그대로 둔다.** `URLSearchParams` 로 만들면 `%2C`·`%2A` 로 부풀어서
 * 40手 줄의 주소가 두 배가 되는데, 둘 다 쿼리에 그냥 쓸 수 있는 글자다(RFC 3986 sub-delims).
 */
export const routeExplore = (handicap: string, moves: readonly string[]): string => {
  const q: string[] = [];
  if (handicap) q.push(`${EXPLORE_PARAM_HANDICAP}=${handicap}`);
  if (moves.length > 0) q.push(`${EXPLORE_PARAM_MOVES}=${moves.join(',')}`);
  return q.length === 0 ? ROUTE_EXPLORE : `${ROUTE_EXPLORE}?${q.join('&')}`;
};

/**
 * 안내. **판도 사람도 안 부르는 유일한 화면이다** — 서버에 아무것도 안 묻고, 로그인도 안
 * 본다. 주소가 있어야 하는 이유가 그래서 하나 더 있다: 검색 결과와 공유 링크가 여기로 온다.
 */
export const ROUTE_GUIDE = '/guide';

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

/**
 * 방 하나. **주소에 들어가는 값이 곧 열쇠다** — 영숫자 8자 난수라 유추할 수 없고, 그래서
 * 이 주소를 아는 것이 입장 자격의 절반이다(나머지 절반은 로그인과 정원 2명).
 *
 * `/reviews/:id` 와 달리 **숫자가 아니다.** 연번이면 로그인한 아무나 남의 방을 훑어볼 수
 * 있고, 그 순간 이 기능의 전제가 무너진다.
 */
export const routeRoom = (id: string): string => `${ROOMS_SEGMENT_PATH}/${id}`;

/** `/rooms/:id` 의 첫 조각. */
export const ROOMS_SEGMENT = 'rooms';

const ROOMS_SEGMENT_PATH = `/${ROOMS_SEGMENT}`;

/** `/reviews/:id` 의 첫 조각. `parseRoute` 와 위 상수가 같은 낱말을 쓰게 묶어 둔다. */
export const REVIEWS_SEGMENT = 'reviews';

/** `/reviews/:id/quiz` 의 마지막 조각. */
export const QUIZ_SEGMENT = 'quiz';

/** `/me` 의 첫 조각. */
export const ME_SEGMENT = 'me';

/** `/guide` 의 첫 조각. */
export const GUIDE_SEGMENT = 'guide';
