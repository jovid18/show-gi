import { describe, expect, it } from 'vitest';

import type { Player, Snapshot, Status } from '@/protocol/game';

import { candidateRays, resultText } from './board-view';

/** 결과 문구는 `status` 와 `winner` 만 본다. 나머지는 읽지 않으므로 최소한만 채운다. */
function ended(status: Status, winner?: Player): Snapshot {
  const base: Snapshot = {
    sfen: '',
    ply: 0,
    turn: 'b',
    yourColor: 'b',
    yourTurn: false,
    inCheck: false,
    thinking: false,
    legalMoves: null,
    moves: null,
    judging: false,
    status,
    undoLeft: 0,
    hintLeft: 0,
    canHint: false,
    canUndo: false,
  };
  // 中断에는 승자가 아예 오지 않는다. `winner: undefined` 는 「빈 승자」라는 없는 값이다.
  return winner ? { ...base, winner } : base;
}

describe('resultText', () => {
  it('中断は勝ち負けを言わない', () => {
    // 서버가 상대의 수를 구하지 못해 접은 판이다. 「相手が投了しました」로 그리면 지고
    // 있던 판이 화면에서 이긴 판이 된다.
    const text = resultText(ended('aborted'));

    expect(text).not.toBeNull();
    expect(text).not.toContain('勝ち');
    expect(text).not.toContain('負け');
    expect(text).not.toContain('投了');
  });

  it('相手が投了した局は勝ちのままだ', () => {
    // 中断과 갈리는 자리. 엔진이 스스로 `resign` 이라고 답한 것은 정말로 사람의 승리다.
    expect(resultText(ended('resigned', 'human'))).toContain('あなたの勝ちです');
  });

  it('두는 중에는 결과 문구가 없다', () => {
    expect(resultText(ended('playing'))).toBeNull();
  });
});

describe('candidateRays', () => {
  it('ranks the first three candidates and carries the dropped piece', () => {
    const rays = candidateRays([{ usi: '7g7f' }, { usi: 'P*5e' }, { usi: '2g2f' }, { usi: '3g3f' }], 'human');
    expect(rays.map((r) => r.rank)).toEqual([1, 2, 3]);
    expect(rays[1]).toMatchObject({ from: null, drop: 'P', by: 'human' });
    expect(rays[0]?.drop).toBeUndefined();
  });

  it('keeps the list rank when a candidate cannot be read', () => {
    // 목록의 순위 숫자와 화살표 색이 같아야 한다. 읽지 못한 줄 때문에 3위가 2위 색을 받으면 안 된다.
    const rays = candidateRays([{ usi: '7g7f' }, { usi: '' }, { usi: '2g2f' }], 'engine');
    expect(rays.map((r) => r.rank)).toEqual([1, 3]);
  });
});
