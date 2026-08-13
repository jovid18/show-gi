import { describe, expect, it } from 'vitest';

import { branchMotion, branchStatusJa, playerCp, rankOf, rowScoreJa, scoreJa, stepMotion } from './branch';
import type { WhatIfNode } from '@/protocol/whatif';
import { fromUsi, toIndex } from '@/models/square';

const sq = (usi: string): number => toIndex(fromUsi(usi));

// 手数 하나를 넘어갈 때 필요한 것은 USI 하나뿐이다 — 기보든 분기든 같은 함수가 본다.
function moves(...usis: string[]): { usi: string }[] {
  return usis.map((usi) => ({ usi }));
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
  turn: 'b',
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

// 두 화면(대국 중의 블런더 목록 · 되짚기의 「この局面で指せた手」)이 이 두 함수를 같이 쓴다.
// **부호 규칙이 두 벌이면 한쪽만 낡는다** — 실제로 되짚기 쪽이 뒤집기 없이 자라 있었다.
describe('rowScoreJa', () => {
  // 手数는 세는 값이라 관점을 바꿔도 자가 안 갈린다. 안 뒤집으면 **상대의 詰み을 내 詰み으로**
  // 말하게 된다 — `lets_mate` 카테고리 전체가 그 자리다.
  it('상대가 두는 자리면 詰み의 주어가 바뀐다', () => {
    const row = { cp: 30000, mateIn: 3 };
    expect(rowScoreJa(row, false)).toBe('3手で詰み');
    expect(rowScoreJa(row, true)).toBe('3手で詰まされる');
  });

  // cp는 열의 자(둔 쪽 관점)를 지킨다. 뒤집는 것은 詰み의 주어와 색뿐이다.
  it('詰み이 아니면 cp를 그대로 적는다', () => {
    expect(rowScoreJa({ cp: -151, mateIn: undefined }, true)).toBe('-151');
    expect(rowScoreJa({ cp: -151, mateIn: undefined }, false)).toBe('-151');
  });
});

describe('playerCp', () => {
  // 파랑·빨강은 어디서나 「나에게 좋은가」다. 상대 차례의 국면에서 안 뒤집으면 색이 통째로
  // 반대가 되고, **상대의 결정타가 가장 파랗게** 나온다.
  it('상대가 두는 자리면 색의 부호가 뒤집힌다', () => {
    expect(playerCp({ cp: -151, mateIn: undefined }, true)).toBe(151);
    expect(playerCp({ cp: -151, mateIn: undefined }, false)).toBe(-151);
  });

  // 詰み의 cp는 환산값(±30000)이라 ±800 자에 얹으면 언제나 양 끝이다. 그 줄은 말로만 선다.
  it('詰み은 색으로 말하지 않는다', () => {
    expect(playerCp({ cp: 29990, mateIn: 1 }, false)).toBeUndefined();
  });

  it('값이 없으면 색도 없다', () => {
    expect(playerCp({ cp: undefined, mateIn: undefined }, false)).toBeUndefined();
  });
});

// **詰み이 cp보다 언제나 바깥이다.** cp만으로 세우면 「3手で詰み」과 「+2900」이 이웃으로
// 서는데 그 둘은 이웃이 아니다. 그리고 **빨리 죽는 쪽이 더 나쁘다**.
describe('rankOf', () => {
  it('詰み이 어떤 cp보다 위다', () => {
    expect(rankOf({ cp: undefined, mateIn: 7 })).toBeGreaterThan(rankOf({ cp: 29000, mateIn: undefined }));
  });

  it('빨리 詰ます 쪽이 위다', () => {
    expect(rankOf({ cp: undefined, mateIn: 1 })).toBeGreaterThan(rankOf({ cp: undefined, mateIn: 9 }));
  });

  // 부호만 보고 자르면 이 순서가 뒤집힌다 — 빨리 죽는 쪽이 더 나쁜 자리다.
  it('빨리 詰まされる 쪽이 맨 아래다', () => {
    expect(rankOf({ cp: undefined, mateIn: -1 })).toBeLessThan(rankOf({ cp: undefined, mateIn: -9 }));
  });

  // 값이 없는 줄은 맨 아래다. 0으로 채우면 호각으로 읽히고 목록 가운데에 선다.
  it('값이 없으면 맨 아래다', () => {
    expect(rankOf({ cp: undefined, mateIn: undefined })).toBeLessThan(rankOf({ cp: -29000, mateIn: undefined }));
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

  // **상대 차례에도 못 두게 하지 않는다.** 「상대라면 어떻게 둘까」를 둬 보는 것이
  // 이 화면의 내용이라, 그 자리에서 손을 놓게 만들면 절반이 사라진다.
  it('상대 차례면 상대의 수도 둬 보라고 말한다', () => {
    expect(branchStatusJa(node({ turn: 'w', yourTurn: false }), false)).toContain('相手の手も');
  });
});
