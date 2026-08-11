import { describe, expect, it } from 'vitest';

import { branchMotion, branchStatusJa, scoreJa, stepMotion } from './branch';
import type { ReviewMove, WhatIfNode } from './protocol';
import { fromUsi, toIndex } from '@/shogi/square';

const sq = (usi: string): number => toIndex(fromUsi(usi));

function moves(...usis: string[]): ReviewMove[] {
  return usis.map((usi, i) => ({ ply: i + 1, usi, ja: '', by: i % 2 === 0 ? 'human' : 'engine' }));
}

describe('stepMotion', () => {
  it('한 수 나아가면 그 수가 간 방향으로 난다', () => {
    const kifu = moves('7g7f', '3c3d');
    expect(stepMotion(kifu, 0, 1, 1)).toEqual({ from: sq('7g'), to: sq('7f'), id: 1 });
    expect(stepMotion(kifu, 1, 2, 2)).toEqual({ from: sq('3c'), to: sq('3d'), id: 2 });
  });

  // 되감는 것도 판 위에서 벌어지는 일이다. 방향이 반대라는 것이 곧 사실이다.
  it('한 수 되감으면 방향이 뒤집힌다', () => {
    expect(stepMotion(moves('7g7f'), 1, 0, 3)).toEqual({ from: sq('7f'), to: sq('7g'), id: 3 });
  });

  // **뛰어넘는 것은 한 수가 아니다.** 슬라이더로 40手를 건너뛰는 자리에 움직임을 그리면
  // 있지도 않았던 한 수를 그리게 된다.
  it('뛰어넘으면 아무것도 안 난다', () => {
    const kifu = moves('7g7f', '3c3d', '8h2b+');
    expect(stepMotion(kifu, 0, 3, 4)).toBeNull();
    expect(stepMotion(kifu, 3, 0, 5)).toBeNull();
    expect(stepMotion(kifu, 1, 1, 6)).toBeNull();
  });

  // 打은 판 위에 출발 칸이 없다. 되감으면 駒가 駒台로 돌아가므로 판에는 그릴 것이 없다.
  it('打은 떨어지고, 되감으면 안 그린다', () => {
    const kifu = moves('P*5e');
    expect(stepMotion(kifu, 0, 1, 7)).toEqual({ from: null, to: sq('5e'), id: 7 });
    expect(stepMotion(kifu, 1, 0, 8)).toBeNull();
  });

  // 못 읽는 좌표로 엉뚱한 칸에서 駒가 날아오면 「무엇이 어디서 왔나」를 틀리게 가르친다.
  it('읽을 수 없는 수는 안 그린다', () => {
    expect(stepMotion(moves('zzzz'), 0, 1, 9)).toBeNull();
    expect(stepMotion([], 0, 1, 10)).toBeNull();
  });
});

const node = (over: Partial<WhatIfNode> = {}): WhatIfNode => ({
  basePly: 4,
  ply: 4,
  sfen: '',
  yourTurn: true,
  status: 'playing',
  legalMoves: [],
  line: [],
  candidates: [],
  ...over,
});

describe('branchMotion', () => {
  it('분기의 마지막 수가 판 위에서 난다', () => {
    const n = node({
      line: [
        { ply: 5, usi: '8h2b+', ja: '▲2二角成', by: 'human', sfen: '' },
        { ply: 6, usi: '3a2b', ja: '△同銀', by: 'engine', sfen: '' },
      ],
    });
    expect(branchMotion(n, 11)).toEqual({ from: sq('3a'), to: sq('2b'), id: 11 });
  });

  it('아직 한 수도 안 둔 자리에서는 안 난다', () => {
    expect(branchMotion(node(), 12)).toBeNull();
  });
});

// **詰み은 cp로 말하지 않는다.** 30000은 평가치가 아니라 환산값이고, 초심자에게
// 「+30000」은 아무것도 아니다.
describe('scoreJa', () => {
  it('詰み은 手数로 말한다', () => {
    expect(scoreJa(30000, 5)).toBe('5手で詰み');
    expect(scoreJa(-30000, -3)).toBe('3手で詰まされる');
  });

  it('평가치는 부호까지 적는다', () => {
    expect(scoreJa(120, undefined)).toBe('+120');
    expect(scoreJa(-40, 0)).toBe('-40');
    expect(scoreJa(0, undefined)).toBe('0');
  });

  // 없는 값을 0으로 채우지 않는다 — 0은 호각이라는 **다른 사실**이다.
  it('값이 없으면 빈 문자열', () => {
    expect(scoreJa(undefined, undefined)).toBe('');
  });
});

describe('branchStatusJa', () => {
  it('읽는 중이 다른 무엇보다 먼저다', () => {
    expect(branchStatusJa(node({ status: 'checkmate' }), true)).toBe('読んでいます…');
  });

  // 詰み은 **누구 차례인가**로 승패가 갈린다. 뒤집히면 진 판이 이긴 판으로 보인다.
  it('詰み은 차례로 승패가 갈린다', () => {
    expect(branchStatusJa(node({ status: 'checkmate', yourTurn: true }), false)).toContain('負け');
    expect(branchStatusJa(node({ status: 'checkmate', yourTurn: false }), false)).toContain('勝ち');
  });

  it('둘 차례면 판 위에서 두라고 말한다', () => {
    expect(branchStatusJa(node(), false)).toContain('あなたの番');
  });
});
