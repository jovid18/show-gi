// 주소 하나가 화면 하나. **라이브러리를 안 쓴다.**
//
// 경로가 셋이고 중첩도 loader도 없는데, 그보다 큰 이유가 있다 — **대국 화면은 언마운트하면
// 안 된다.** WebSocket 하나에 매여 있어서 내리면 연결이 끊기고 그 판이 `abandoned` 로
// 닫힌다(App.tsx). 그래서 라우트가 element를 갈아 끼우는 방식을 애초에 못 쓰고, 대국은
// 라우트 밖에 상시 마운트로 두고 **감추기만** 한다. 라이브러리를 얹어도 그 예외는 그대로
// 남으므로, 얹으면 규칙이 둘(라우터의 것 + 이 예외)이 된다.
//
// 딥링크는 서버가 받쳐 준다 — Caddy가 `try_files {path} /index.html` 이고 vite dev도
// 같은 폴백이다. 새로고침으로 `/reviews/12` 에 들어와도 앱이 뜬다.

import { useEffect, useState } from 'react';

import {
  GUIDE_SEGMENT,
  ME_SEGMENT,
  QUIZ_SEGMENT,
  REVIEWS_SEGMENT,
  ROUTE_GAME,
  ROUTE_GUIDE,
  ROUTE_ME,
  ROUTE_REVIEWS,
  routeQuiz,
  routeReview,
} from './const';

/**
 * 지금 어느 화면인가.
 *
 * `review` 가 id를 들고 있는 것이 요점이다 — 어느 판을 보고 있는지가 **주소에 있어야**
 * 새로고침과 뒤로 가기가 맞는다. 화면 안의 상태로 두면 둘 다 목록으로 튕긴다.
 */
export type Route =
  | { name: 'game' }
  | { name: 'reviews' }
  // ply 는 **열 때의 手数**다. 없으면 0手目(시작 국면)에서 연다.
  | { name: 'review'; id: number; ply?: number }
  | { name: 'quiz'; id: number }
  | { name: 'me' }
  | { name: 'guide' };

/** 주소 → 화면. **못 읽는 주소는 대국이다** — 404 화면을 만들 만큼 경로가 많지 않다. */
export function parseRoute(pathname: string): Route {
  const parts = pathname.split('/').filter(Boolean);
  if (parts[0] === GUIDE_SEGMENT) return { name: 'guide' };
  if (parts[0] === ME_SEGMENT) return { name: 'me' };
  if (parts[0] !== REVIEWS_SEGMENT) return { name: 'game' };
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
  }
}

/**
 * 주소를 바꾼다. 같은 주소면 아무것도 하지 않는다 — 이력에 같은 자리를 쌓으면
 * 뒤로 가기를 여러 번 눌러야 한다.
 */
export function navigate(route: Route): void {
  const href = hrefOf(route);
  if (href === window.location.pathname) return;
  window.history.pushState(null, '', href);
  // `pushState` 는 이벤트를 안 낸다. 구독한 쪽이 알 길이 없어서 우리가 하나 낸다.
  window.dispatchEvent(new PopStateEvent('popstate'));
}

/** 지금 화면. 뒤로/앞으로 가기와 `navigate` 를 둘 다 듣는다. */
export function useRoute(): Route {
  const [pathname, setPathname] = useState(() => window.location.pathname);

  useEffect(() => {
    const onPop = (): void => setPathname(window.location.pathname);
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  return parseRoute(pathname);
}
