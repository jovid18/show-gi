import { describe, expect, it } from 'vitest';

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
});
