import { describe, expect, it } from 'vitest';

import { ROUTE_HOME, ROUTE_REVIEWS, routeExplore, routeQuiz, routeReview, routeRoom } from './const';
import { hrefOf, parseRoute } from './router';

// 주소를 읽는 쪽은 새로고침과 뒤로 가기가 지나가는 유일한 문이다. 조용히 틀리면
// 「그 판을 열었는데 목록이 뜬다」로 나타나고, 화면에서는 버그로 안 보인다.
describe('parseRoute', () => {
  it('빈 경로와 루트는 홈이다', () => {
    expect(parseRoute('/')).toEqual({ name: 'home' });
    expect(parseRoute('')).toEqual({ name: 'home' });
  });

  // 여기가 어긋나면 홈과 대국이 통째로 자리를 바꾼다(journal §86).
  it('대국은 /play 다', () => {
    expect(parseRoute('/play')).toEqual({ name: 'game' });
    expect(parseRoute('/play/')).toEqual({ name: 'game' });
    // 꼬리가 붙어도 대국이다 — 이 화면은 주소에 무엇도 안 싣는다.
    expect(parseRoute('/play/nope')).toEqual({ name: 'game' });
  });

  it('목록', () => {
    expect(parseRoute('/reviews')).toEqual({ name: 'reviews' });
    // 끝의 슬래시가 다른 화면이 되지 않아야 한다
    expect(parseRoute('/reviews/')).toEqual({ name: 'reviews' });
  });

  it('판 하나', () => {
    expect(parseRoute('/reviews/12')).toEqual({ name: 'review', id: 12 });
  });

  it('id가 정수가 아니면 목록이다', () => {
    // 상세를 부르면 그 id로 서버 요청이 나가므로, 여기서 걸러야 한다
    for (const bad of [
      '/reviews/abc',
      '/reviews/0',
      '/reviews/-3',
      '/reviews/1.5',
      '/reviews/1e3',
      '/reviews/01',
      '/reviews/ 1',
    ]) {
      expect(parseRoute(bad)).toEqual({ name: 'reviews' });
    }
  });

  it('모르는 경로는 홈이다 — 404 화면을 두지 않는다', () => {
    expect(parseRoute('/nope')).toEqual({ name: 'home' });
  });

  it('퀴즈', () => {
    expect(parseRoute('/reviews/12/quiz')).toEqual({ name: 'quiz', id: 12 });
    expect(parseRoute('/reviews/12/quiz/')).toEqual({ name: 'quiz', id: 12 });
  });

  // 퀴즈가 아닌 꼬리는 그 판이다. 목록으로 튕기면 링크를 잘못 복사한 사람이 판을
  // 잃고, 이 앱에 404 화면은 없다.
  it('모르는 꼬리는 그 판이다', () => {
    expect(parseRoute('/reviews/12/nope')).toEqual({ name: 'review', id: 12 });
  });

  // id 검사가 꼬리보다 먼저다 — 안 그러면 못 읽는 id로 퀴즈 요청이 나간다.
  it('id가 정수가 아니면 꼬리가 있어도 목록이다', () => {
    expect(parseRoute('/reviews/abc/quiz')).toEqual({ name: 'reviews' });
  });

  // 숫자 꼬리는 열 手数다. 총평이 짚은 국면으로 곧장 들어오는 링크가 이 모양이다.
  it('숫자 꼬리는 手数다', () => {
    expect(parseRoute('/reviews/12/81')).toEqual({ name: 'review', id: 12, ply: 81 });
    // 0手目(시작 국면)은 유효한 자리다 — id 와 갈리는 지점이다.
    expect(parseRoute('/reviews/12/0')).toEqual({ name: 'review', id: 12, ply: 0 });
  });

  it('마이페이지', () => {
    expect(parseRoute('/me')).toEqual({ name: 'me' });
    expect(parseRoute('/me/')).toEqual({ name: 'me' });
  });

  // 마이페이지에는 판이 없으므로 꼬리도 없다. 못 읽는 주소가 대국인 것과 같은 판단으로,
  // 여기서는 마이페이지 자신이다 — 404 화면은 이 앱에 없다.
  it('마이페이지의 꼬리는 그대로 마이페이지다', () => {
    expect(parseRoute('/me/anything')).toEqual({ name: 'me' });
  });

  // 안내는 검색 결과와 공유 링크가 들어오는 자리라, 꼬리가 붙어 와도 안내여야 한다.
  it('안내', () => {
    expect(parseRoute('/guide')).toEqual({ name: 'guide' });
    expect(parseRoute('/guide/')).toEqual({ name: 'guide' });
    expect(parseRoute('/guide/anything')).toEqual({ name: 'guide' });
  });

  // 취해 오기도 꼬리를 안 본다. 주소가 아무것도 안 든다 — 붙여 넣은 글은 화면 안에만 있다.
  it('취해 오기', () => {
    expect(parseRoute('/import')).toEqual({ name: 'import' });
    expect(parseRoute('/import/')).toEqual({ name: 'import' });
    expect(parseRoute('/import/anything')).toEqual({ name: 'import' });
  });

  // 검토는 쿼리를 보는 유일한 화면이다. 여기가 틀리면 링크로 받은 국면이 안 열린다.
  it('검토 — 手合割과 수순이 쿼리에 있다', () => {
    expect(parseRoute('/explore')).toEqual({ name: 'explore', handicap: '', moves: [] });
    expect(parseRoute('/explore/')).toEqual({ name: 'explore', handicap: '', moves: [] });
    expect(parseRoute('/explore?h=nimaiochi')).toEqual({ name: 'explore', handicap: 'nimaiochi', moves: [] });
    expect(parseRoute('/explore?h=nimaiochi&m=7g7f,3c3d')).toEqual({
      name: 'explore',
      handicap: 'nimaiochi',
      moves: ['7g7f', '3c3d'],
    });
    // 打과 成도 수다 — `*` 와 `+` 가 주소에서 살아 돌아와야 한다
    expect(parseRoute('/explore?m=2b3c+,P*5e')).toEqual({
      name: 'explore',
      handicap: '',
      moves: ['2b3c+', 'P*5e'],
    });
  });

  // 한 토큰이 깨지면 줄 전체를 버린다. 절반만 두면 링크를 받은 사람이 보는 판과
  // 준 사람이 본 판이 다르고, 그건 화면에서 버그로 안 보인다.
  it('모양이 아닌 수순은 통째로 버린다', () => {
    for (const bad of [
      '/explore?m=7g7f,zzz',
      '/explore?m=7g7f,,3c3d',
      '/explore?m=7g7f 3c3d',
      // 手合割 id 는 주소에 실릴 수 있는 모양까지만 본다 — 이건 그 모양이 아니다
      `/explore?h=${'a'.repeat(33)}`,
    ]) {
      expect(parseRoute(bad)).toEqual({ name: 'explore', handicap: '', moves: [] });
    }
  });

  // 없는 手合割을 여기서 자르지 않는다. 목록에 있는지는 서버가 정하고(`bad_handicap`),
  // 화면이 어휘를 한 벌 더 들면 새 手合이 붙는 날 그 공유 링크가 조용히 平手로 열린다.
  it('모르는 手合割 id 는 서버에 넘긴다', () => {
    expect(parseRoute('/explore?h=hachimaiochi2&m=7g7f')).toEqual({
      name: 'explore',
      handicap: 'hachimaiochi2',
      moves: ['7g7f'],
    });
  });

  // 방 id 는 영숫자 8자 다(서버의 NewRoomID). 모양이 아니면 대국으로 보낸다 —
  // 아무 문자열이나 통과시키면 그 값이 그대로 `fetch` 의 경로가 된다.
  it('방', () => {
    const id = 'AbCdEf12';
    expect(parseRoute(`/rooms/${id}`)).toEqual({ name: 'room', id });
  });

  it('모양이 아닌 방 id 는 홈이다', () => {
    // `-`·`_` 도 여기서 걸린다 — 알파벳에 없는 글자다(roomIDAlphabet).
    const paths = [
      '/rooms',
      '/rooms/',
      '/rooms/12',
      '/rooms/short',
      '/rooms/' + 'a'.repeat(9),
      '/rooms/a+b',
      '/rooms/aaaa-_aa',
    ];
    for (const path of paths) {
      expect(parseRoute(path)).toEqual({ name: 'home' });
    }
  });
});

describe('hrefOf', () => {
  it('읽은 것을 다시 적으면 같은 주소다', () => {
    for (const path of [
      '/',
      '/play',
      '/reviews',
      '/reviews/12',
      '/reviews/12/quiz',
      '/me',
      '/guide',
      '/import',
      '/rooms/AbCdEf12',
    ]) {
      expect(hrefOf(parseRoute(path))).toBe(path);
    }
  });

  // 검토는 쿼리까지 왕복해야 한다 — 주소가 판을 들고 있는 유일한 화면이다.
  it('검토도 왕복한다', () => {
    for (const path of ['/explore', '/explore?h=nimaiochi', '/explore?h=nimaiochi&m=7g7f,3c3d', '/explore?m=P*5e']) {
      expect(hrefOf(parseRoute(path))).toBe(path);
    }
  });

  // 사진에서 읽어 온 국면도 주소가 든다(journal §129). 왕복이 안 맞으면 새로고침 한 번에
  // 확인까지 끝낸 판이 사라진다.
  it('뿌리 국면도 왕복한다', () => {
    for (const path of [
      `/explore?s=${encodeURIComponent(START_SFEN)}`,
      `/explore?s=${encodeURIComponent(START_SFEN)}&m=7g7f,3c3d`,
    ]) {
      expect(hrefOf(parseRoute(path))).toBe(path);
    }
  });
});

/**
 * 뿌리 국면. 「성립하는 판인가」는 안 본다 — 그 판단의 정본은 서버의 룰 엔진 하나뿐이고,
 * 여기서 한 벌 더 적으면 어긋났을 때 어느 쪽이 맞는지 아무도 모른다.
 */
describe('뿌리 국면', () => {
  it('판이 실리면 그것이 뿌리다', () => {
    expect(parseRoute(`/explore?s=${encodeURIComponent(START_SFEN)}`)).toEqual({
      name: 'explore',
      handicap: '',
      moves: [],
      sfen: START_SFEN,
    });
  });

  it('성립하지 않는 판도 그대로 넘긴다 — 거절은 서버가 한다', () => {
    // 二歩. 모양은 SFEN 이라 여기를 지나고, 서버가 `bad_position` 으로 답한다.
    const nifu = '4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1';
    expect(parseRoute(`/explore?s=${encodeURIComponent(nifu)}`)).toMatchObject({ sfen: nifu });
  });

  it('SFEN 모양이 아니면 뿌리가 없는 것으로 연다', () => {
    for (const bad of ['not-a-position', 'lnsgkgsnl b - 1', `${START_SFEN} extra field here`, '<script>']) {
      expect(parseRoute(`/explore?s=${encodeURIComponent(bad)}`)).toEqual({
        name: 'explore',
        handicap: '',
        moves: [],
      });
    }
  });

  // 뿌리는 하나여야 한다. 서버가 둘을 같이 받으면 거절하므로(`bad_root`) 주소를 만드는
  // 쪽도 만드는 쪽도 하나만 적는다.
  it('판이 있으면 手合割은 안 실린다', () => {
    expect(routeExplore('nimaiochi', ['7g7f'], START_SFEN)).toBe(`/explore?s=${encodeURIComponent(START_SFEN)}&m=7g7f`);
    expect(parseRoute(`/explore?h=nimaiochi&s=${encodeURIComponent(START_SFEN)}`)).toEqual({
      name: 'explore',
      handicap: '',
      moves: [],
      sfen: START_SFEN,
    });
  });

  it('사진에서 국면을 취해 오는 화면은 주소가 아무것도 안 든다', () => {
    expect(parseRoute('/position')).toEqual({ name: 'position' });
    expect(hrefOf({ name: 'position' })).toBe('/position');
  });
});

const START_SFEN = 'lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1';

// 주소 조각은 서버가 준 값이다. 타입이 number 라도 JSON 은 그것을 보장하지 않으므로,
// 주소를 만드는 쪽이 막아야 한다 — 안 막으면 프로토콜 상대 주소가 만들어져 링크가
// 밖으로 나간다(CodeQL 의 js/client-side-unvalidated-url-redirection).
describe('주소 조각을 못 믿는다', () => {
  it('판 번호가 숫자가 아니면 목록으로 떨어진다', () => {
    for (const bad of ['//example.com', '1/../..', 'abc', '', '-1', '1.5', Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(routeReview(bad as unknown as number)).toBe(ROUTE_REVIEWS);
      expect(routeQuiz(bad as unknown as number)).toBe(ROUTE_REVIEWS);
    }
  });

  it('手数가 못 믿을 값이면 판만 연다', () => {
    expect(routeReview(12, '//example.com' as unknown as number)).toBe('/reviews/12');
    // 0手目는 멀쩡한 값이다. 총평의 링크가 실제로 그 자리를 가리킨다.
    expect(routeReview(12, 0)).toBe('/reviews/12/0');
  });

  it('방 id 는 영숫자만이다', () => {
    expect(routeRoom('AbCdEf12')).toBe('/rooms/AbCdEf12');
    for (const bad of ['//example.com', 'ab/cd', 'ab-cd', '']) {
      expect(routeRoom(bad)).toBe(ROUTE_HOME);
    }
  });
});
