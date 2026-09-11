// 경로 문자열은 여기만 안다. 트리와 따로 두면 「어떤 주소가 있나」를 파일 하나로 읽는다.

/**
 * 홈. 세로 메뉴 하나가 전부인 화면이다(journal §86).
 *
 * 주소가 `/` 여야 한다. 로그인이 끝나면 서버가 이 주소로 되돌려보내고(`auth.go`),
 * `canonical` 과 `sitemap.xml` 도 여기를 가리킨다.
 */
export const ROUTE_HOME = '/';

/** 대국. `/` 에서 내려왔다(journal §86). 그 자리는 홈이 가졌다. */
export const ROUTE_GAME = '/play';

/** `/play` 의 첫 조각. */
export const PLAY_SEGMENT = 'play';

export const ROUTE_REVIEWS = '/reviews';

/** 마이페이지. 사람 하나를 가리켜서 id가 없다. */
export const ROUTE_ME = '/me';

/**
 * 검토. 판을 주소에 담는 하나뿐인 화면이다.
 *
 * 手合割 하나와 지금까지 둔 수순이 쿼리에 실린다(`?h=nimaiochi&m=7g7f,3c3d`). 새로고침·
 * 뒤로 가기·링크 공유가 그것으로 살아난다.
 *
 * 뿌리가 둘이다. 手合割 id 가 하나이고, 사진에서 읽어 온 국면(`?s=<SFEN>`)이 다른
 * 하나다(journal §129). 뒤엣것은 手合割과 수순으로 표현할 수 없어서 판이 실린다.
 * 남이 준 링크가 「있을 수 없는 판」을 여는 것은 서버가 막는다(`shogi.Faults`).
 */
export const ROUTE_EXPLORE = '/explore';

/** `/explore` 의 첫 조각. */
export const EXPLORE_SEGMENT = 'explore';

/** 手合割을 싣는 쿼리 이름. 없거나 비면 平手다. */
export const EXPLORE_PARAM_HANDICAP = 'h';

/** 수순을 싣는 쿼리 이름. `,` 로 이어 붙인다. */
export const EXPLORE_PARAM_MOVES = 'm';

/** 뿌리 국면을 싣는 쿼리 이름. 사진에서 읽어 온 판이 여기로 온다. */
export const EXPLORE_PARAM_SFEN = 's';

/**
 * 검토 화면의 주소.
 *
 * `,` 와 `*` 를 그대로 둔다. `URLSearchParams` 로 만들면 `%2C`·`%2A` 로 부풀어 40手 줄의
 * 주소가 두 배가 된다. 둘 다 쿼리에 그냥 쓸 수 있는 글자다(RFC 3986 sub-delims).
 */
export const routeExplore = (handicap: string, moves: readonly string[], sfen = ''): string => {
  const q: string[] = [];
  // 판이 있으면 手合割은 적지 않는다. 뿌리를 정하는 값이 둘이면 서버가 거절한다(`bad_root`).
  if (sfen) q.push(`${EXPLORE_PARAM_SFEN}=${encodeURIComponent(sfen)}`);
  else if (handicap) q.push(`${EXPLORE_PARAM_HANDICAP}=${handicap}`);
  if (moves.length > 0) q.push(`${EXPLORE_PARAM_MOVES}=${moves.join(',')}`);
  return q.length === 0 ? ROUTE_EXPLORE : `${ROUTE_EXPLORE}?${q.join('&')}`;
};

/**
 * 안내. 서버에 아무것도 묻지 않고 로그인도 보지 않는 하나뿐인 화면이다. 그래도 주소가
 * 있는 것은 검색 결과와 공유 링크가 여기로 오기 때문이다.
 */
export const ROUTE_GUIDE = '/guide';

/**
 * 가져오기. 밖에서 둔 자기 기보를 붙여 넣는 화면이다(journal §126).
 *
 * 판도 사람도 주소에 싣지 않는다. 여기서 만들어지는 판은 가져온 뒤에야 번호를 갖고, 그때
 * 화면이 되짚기로 옮겨 간다.
 */
export const ROUTE_IMPORT = '/import';

/**
 * 국면을 사진에서 가져오는 화면(journal §129).
 *
 * 주소가 아무것도 담지 않는다. 올린 그림과 읽어 낸 판은 화면 안에만 있고, 사람이 확인을
 * 끝내면 그 국면이 검토의 주소가 되어(`routeExplore` 의 `s`) 이 화면을 떠난다.
 */
export const ROUTE_POSITION = '/position';

/** `/position` 의 첫 조각. */
export const POSITION_SEGMENT = 'position';

/**
 * 판 하나. 주소에 id가 들어가는 하나뿐인 자리다.
 *
 * `ply` 를 주면 그 手数에서 열린다. 총평이 짚은 국면이 링크가 되려면 手数도 주소에
 * 있어야 한다. 화면 안의 상태로 두면 새로고침에 사라진다.
 */
export const routeReview = (id: number, ply?: number): string => {
  // 판 번호는 1부터다(`games.id` 가 bigserial). 빈 문자열도 여기서 걸린다. Number('') 이
  // 0 이라 하한이 0 이면 그것이 「0번 판」으로 통과한다.
  const safeId = pathNumber(id, 1);
  if (safeId === null) return ROUTE_REVIEWS;
  // 手数는 0부터다. 총평의 「이 국면을 다시 봐라」가 0手目를 가리킬 수 있다.
  const safePly = ply === undefined ? null : pathNumber(ply, 0);
  return safePly === null ? `${ROUTE_REVIEWS}/${safeId}` : `${ROUTE_REVIEWS}/${safeId}/${safePly}`;
};

/**
 * 주소 조각으로 쓸 수 있는 숫자인가. 아니면 `null`.
 *
 * 타입이 `number` 인데 실제 값은 서버가 준 JSON 이다. 문자열이 들어오면 그대로 이어 붙어
 * `/reviews///example.com` 같은 프로토콜 상대 주소가 만들어지고, 그건 외부 이동이다.
 */
const pathNumber = (value: number, min: number): string | null => {
  const n = Number(value);
  return Number.isSafeInteger(n) && n >= min ? String(n) : null;
};

/**
 * 그 판의 퀴즈. 되짚기 아래에 둔다. 주소가 그 판을 담아야 뒤로 가기가 그 판으로 돌아간다.
 */
export const routeQuiz = (id: number): string => {
  const base = routeReview(id);
  // 번호가 붙지 않았으면 그 판의 퀴즈라는 말이 성립하지 않는다. 목록으로 떨어뜨린다.
  return base === ROUTE_REVIEWS ? ROUTE_REVIEWS : `${base}/${QUIZ_SEGMENT}`;
};

/**
 * 방 하나. 주소에 들어가는 값이 곧 열쇠다. 영숫자 8자 난수라 유추할 수 없고, 이 주소를
 * 아는 것이 입장 자격의 절반이다(나머지 절반은 로그인과 정원 2명).
 *
 * 연번으로 두면 로그인한 아무나 남의 방을 훑어볼 수 있다.
 */
export const routeRoom = (id: string): string => (ROOM_ID.test(id) ? `${ROOMS_SEGMENT_PATH}/${id}` : ROUTE_HOME);

/**
 * 방 id 의 모양. 서버가 뽑는 글자와 같다(`internal/match` 의 roomIDAlphabet).
 *
 * 길이를 박지 않는다. 8자는 서버의 선택이고, 그 값이 바뀌면 링크가 경고 없이 홈으로
 * 떨어진다.
 */
const ROOM_ID = /^[A-Za-z0-9]+$/;

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

/** `/import` 의 첫 조각. */
export const IMPORT_SEGMENT = 'import';
