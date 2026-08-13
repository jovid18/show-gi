import { describe, expect, it } from 'vitest';

import { hrefOf, parseRoute } from './router';

// 주소를 읽는 쪽은 **새로고침과 뒤로 가기가 지나가는 유일한 문**이다. 조용히 틀리면
// 「그 판을 열었는데 목록이 뜬다」로 나타나고, 화면에서는 버그로 안 보인다.
describe('parseRoute', () => {
  it('빈 경로와 루트는 대국이다', () => {
    expect(parseRoute('/')).toEqual({ name: 'game' });
    expect(parseRoute('')).toEqual({ name: 'game' });
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

  it('모르는 경로는 대국이다 — 404 화면을 두지 않는다', () => {
    expect(parseRoute('/nope')).toEqual({ name: 'game' });
  });

  it('퀴즈', () => {
    expect(parseRoute('/reviews/12/quiz')).toEqual({ name: 'quiz', id: 12 });
    expect(parseRoute('/reviews/12/quiz/')).toEqual({ name: 'quiz', id: 12 });
  });

  // 퀴즈가 아닌 꼬리는 **그 판**이다. 목록으로 튕기면 링크를 잘못 복사한 사람이 판을
  // 잃고, 이 앱에 404 화면은 없다.
  it('모르는 꼬리는 그 판이다', () => {
    expect(parseRoute('/reviews/12/nope')).toEqual({ name: 'review', id: 12 });
  });

  // id 검사가 꼬리보다 먼저다 — 안 그러면 못 읽는 id로 퀴즈 요청이 나간다.
  it('id가 정수가 아니면 꼬리가 있어도 목록이다', () => {
    expect(parseRoute('/reviews/abc/quiz')).toEqual({ name: 'reviews' });
  });
});

describe('hrefOf', () => {
  it('읽은 것을 다시 적으면 같은 주소다', () => {
    for (const path of ['/', '/reviews', '/reviews/12', '/reviews/12/quiz']) {
      expect(hrefOf(parseRoute(path))).toBe(path);
    }
  });
});
