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
});

describe('hrefOf', () => {
  it('읽은 것을 다시 적으면 같은 주소다', () => {
    for (const path of ['/', '/reviews', '/reviews/12']) {
      expect(hrefOf(parseRoute(path))).toBe(path);
    }
  });
});
