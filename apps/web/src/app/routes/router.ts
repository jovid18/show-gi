// 주소 하나가 화면 하나. **라이브러리를 안 쓴다.**
//
// 경로가 적고 중첩도 loader도 없는데, 그보다 큰 이유가 있다 — **대국 화면은 언마운트하면
// 안 된다.** WebSocket 하나에 매여 있어서 내리면 연결이 끊기고 그 판이 `abandoned` 로
// 닫힌다(App.tsx). 그래서 라우트가 element를 갈아 끼우는 방식을 애초에 못 쓰고, 대국은
// 라우트 밖에 상시 마운트로 두고 **감추기만** 한다. 라이브러리를 얹어도 그 예외는 그대로
// 남으므로, 얹으면 규칙이 둘(라우터의 것 + 이 예외)이 된다.
//
// 딥링크는 서버가 받쳐 준다 — Caddy가 `try_files {path} /index.html` 이고 vite dev도
// 같은 폴백이다. 새로고침으로 `/reviews/12` 에 들어와도 앱이 뜬다.

import { useEffect, useState } from 'react';

import {
  EXPLORE_PARAM_HANDICAP,
  EXPLORE_PARAM_MOVES,
  EXPLORE_SEGMENT,
  GUIDE_SEGMENT,
  ME_SEGMENT,
  PLAY_SEGMENT,
  QUIZ_SEGMENT,
  REVIEWS_SEGMENT,
  ROOMS_SEGMENT,
  ROUTE_GAME,
  ROUTE_GUIDE,
  ROUTE_HOME,
  ROUTE_ME,
  ROUTE_REVIEWS,
  routeExplore,
  routeQuiz,
  routeReview,
  routeRoom,
} from './const';

/**
 * 지금 어느 화면인가.
 *
 * `review` 가 id를 들고 있는 것이 요점이다 — 어느 판을 보고 있는지가 **주소에 있어야**
 * 새로고침과 뒤로 가기가 맞는다. 화면 안의 상태로 두면 둘 다 목록으로 튕긴다.
 */
export type Route =
  // 홈. **아무것도 안 부르는 메뉴 하나다**(journal §86).
  | { name: 'home' }
  | { name: 'game' }
  | { name: 'reviews' }
  // ply 는 **열 때의 手数**다. 없으면 0手目(시작 국면)에서 연다.
  | { name: 'review'; id: number; ply?: number }
  | { name: 'quiz'; id: number }
  | { name: 'me' }
  | { name: 'guide' }
  // 검토. **주소가 판을 든다** — 手合割과 지금까지의 수순이 쿼리에 있고, 그래서 이 화면만
  // 라우트가 `?` 뒤를 본다(routes/const.ts 의 routeExplore).
  | { name: 'explore'; handicap: string; moves: string[] }
  // 방 하나. **id 가 문자열인 유일한 라우트다** — 판 번호와 달리 이 값은 난수이고,
  // 그것이 유추를 막는 장치의 전부다(routes/const.ts 의 routeRoom).
  | { name: 'room'; id: string };

/**
 * USI 수 하나의 모양. 판 위의 이동(`7g7f`·`2b3c+`)이거나 持ち駒를 놓는 수(`P*5e`)다.
 *
 * **주소에서 온 값을 검사하는 자리다.** 남이 준 링크의 쿼리가 그대로 요청 본문이 되므로,
 * 모양이 아닌 토큰이 하나라도 있으면 그 줄을 안 쓴다 — 서버가 어차피 거절하지만
 * (explore.go) 그때는 판이 안 서고 에러만 남는다.
 */
const USI_MOVE = /^(?:[1-9][a-i][1-9][a-i]\+?|[PLNSGBR]\*[1-9][a-i])$/;

/**
 * 쿼리에서 값 하나를 꺼낸다.
 *
 * **`URLSearchParams` 를 안 쓴다.** 그쪽은 폼 인코딩이라 **`+` 를 공백으로 읽고**, 그러면
 * 成을 표시하는 `2b3c+` 가 `2b3c `가 되어 수 하나가 모양 검사에서 떨어진다 — 그 하나 때문에
 * 줄 전체가 버려진다(아래). 손으로 가르고 `decodeURIComponent` 로 풀면 `+` 는 그대로
 * 남고 `%2B` 도 풀려서, 주소를 사람이 읽을 수 있는 모양으로 쓸 수 있다(routeExplore).
 */
function queryValue(search: string, name: string): string {
  for (const pair of search.replace(/^\?/, '').split('&')) {
    const eq = pair.indexOf('=');
    if (eq === -1 || pair.slice(0, eq) !== name) continue;
    try {
      return decodeURIComponent(pair.slice(eq + 1));
    } catch {
      // 깨진 `%` 이스케이프. 못 읽는 값은 없는 것으로 둔다 — 아래 모양 검사가 어차피 자른다.
      return '';
    }
  }
  return '';
}

/**
 * 주소의 쿼리에서 검토 화면을 읽는다.
 *
 * **한 토큰이라도 모양이 아니면 줄 전체를 버린다.** 절반만 두면 사람이 링크로 받은 국면과
 * 화면이 다른 판을 그리고, 그게 「이 수를 뒀는데 왜 이 국면이지」가 된다.
 */
function exploreRouteOf(search: string): Route {
  const handicap = queryValue(search, EXPLORE_PARAM_HANDICAP);
  const raw = queryValue(search, EXPLORE_PARAM_MOVES);
  const moves = raw === '' ? [] : raw.split(',');
  // **手合割 id 는 「주소에 실릴 수 있는 모양인가」까지만 본다.** 목록에 있는지는 서버가
  // 정하고(`bad_handicap`), 여기서 어휘를 한 벌 더 적으면 八枚落ち 같은 id 가 붙는 날
  // **그 手合의 공유 링크가 전부 조용히 平手 0手目로 열린다** — 수순까지 함께 버려진다.
  const ok = /^[A-Za-z0-9_-]{0,32}$/.test(handicap) && moves.every((m) => USI_MOVE.test(m));
  return ok ? { name: 'explore', handicap, moves } : { name: 'explore', handicap: '', moves: [] };
}

/**
 * 주소 → 화면. **못 읽는 주소는 홈이다** — 404 화면을 만들 만큼 경로가 많지 않고,
 * 홈은 갈 곳을 전부 세워 놓은 자리라 길을 잃은 사람이 떨어질 곳으로 맞다.
 *
 * 받는 것은 `pathname` **+ `search`** 다. 쿼리를 보는 화면이 검토 하나뿐이라 그쪽만
 * 갈라 읽고, 나머지는 지금까지처럼 경로만으로 정해진다.
 */
export function parseRoute(url: string): Route {
  const cut = url.indexOf('?');
  const pathname = cut === -1 ? url : url.slice(0, cut);
  const search = cut === -1 ? '' : url.slice(cut);

  const parts = pathname.split('/').filter(Boolean);
  if (parts[0] === PLAY_SEGMENT) return { name: 'game' };
  if (parts[0] === EXPLORE_SEGMENT) return exploreRouteOf(search);
  if (parts[0] === GUIDE_SEGMENT) return { name: 'guide' };
  if (parts[0] === ME_SEGMENT) return { name: 'me' };
  // **글자를 확인하고 넘긴다.** 서버가 어차피 404로 답하지만, 아무 문자열이나 그대로
  // 주소에 실으면 그 값이 `fetch` 의 경로가 되고 화면이 못 읽는 답을 받는다.
  // 영숫자 8자가 방 id 의 모양이다(서버의 newRoomID).
  if (parts[0] === ROOMS_SEGMENT) {
    const id = parts[1] ?? '';
    return /^[A-Za-z0-9]{8}$/.test(id) ? { name: 'room', id } : { name: 'home' };
  }
  if (parts[0] !== REVIEWS_SEGMENT) return { name: 'home' };
  if (parts.length === 1) return { name: 'reviews' };

  // **글자를 먼저 본다.** `Number()` 로 바로 바꾸면 `1e3` 이 1000을 통과시켜서, 주소에
  // 적힌 것과 실제로 여는 판이 달라진다(테스트가 이걸 잡았다). `01` 도 같은 이유로 막는다 —
  // 같은 판에 주소가 두 벌 생긴다.
  const id = parts[1] ?? '';
  if (!/^[1-9]\d*$/.test(id)) return { name: 'reviews' };
  // **못 읽는 세 번째 조각은 그 판이다.** 퀴즈가 아닌 무엇이 붙어 있어도 판은 열 수 있고,
  // 그것이 404 화면을 만들지 않기로 한 것과 같은 판단이다.
  //
  // 숫자면 **열 手数**다. `0` 을 허용하는 것이 id 와 갈리는 자리다 — 시작 국면이
  // 유효한 자리이고, 여기는 「없는 판」이 될 수 없다.
  if (parts.length > 2) {
    const tail = parts[2] ?? '';
    if (tail === QUIZ_SEGMENT) return { name: 'quiz', id: Number(id) };
    if (/^(0|[1-9]\d*)$/.test(tail)) return { name: 'review', id: Number(id), ply: Number(tail) };
    return { name: 'review', id: Number(id) };
  }
  return { name: 'review', id: Number(id) };
}

/** 화면 → 주소. **`<a href>` 에 그대로 넣는다** — 가운데 클릭과 링크 복사가 살아 있어야 한다. */
export function hrefOf(route: Route): string {
  switch (route.name) {
    case 'home':
      return ROUTE_HOME;
    case 'game':
      return ROUTE_GAME;
    case 'reviews':
      return ROUTE_REVIEWS;
    case 'review':
      return routeReview(route.id, route.ply);
    case 'quiz':
      return routeQuiz(route.id);
    case 'me':
      return ROUTE_ME;
    case 'guide':
      return ROUTE_GUIDE;
    case 'explore':
      return routeExplore(route.handicap, route.moves);
    case 'room':
      return routeRoom(route.id);
  }
}

/** 지금 주소. **쿼리까지다** — 검토 화면이 판을 그 뒤에 들고 있다. */
function currentURL(): string {
  return window.location.pathname + window.location.search;
}

/**
 * 주소를 바꾼다. 같은 주소면 아무것도 하지 않는다 — 이력에 같은 자리를 쌓으면
 * 뒤로 가기를 여러 번 눌러야 한다.
 *
 * `replace` 는 **이력을 쌓지 않고** 지금 자리를 고쳐 쓴다. 검토에서 한 수 둘 때가 그렇다:
 * 주소는 공유·새로고침을 위해 따라와야 하지만, 40手를 걸어 본 사람이 화면을 벗어나려고
 * 뒤로 가기를 40번 눌러야 하는 것은 아니다.
 */
export function navigate(route: Route, options?: { replace?: boolean }): void {
  const href = hrefOf(route);
  if (href === currentURL()) return;
  if (options?.replace) window.history.replaceState(null, '', href);
  else window.history.pushState(null, '', href);
  // `pushState`·`replaceState` 는 이벤트를 안 낸다. 구독한 쪽이 알 길이 없어서 우리가 하나 낸다.
  window.dispatchEvent(new PopStateEvent('popstate'));
}

/** 지금 화면. 뒤로/앞으로 가기와 `navigate` 를 둘 다 듣는다. */
export function useRoute(): Route {
  const [url, setURL] = useState(currentURL);

  useEffect(() => {
    const onPop = (): void => setURL(currentURL());
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  return parseRoute(url);
}
