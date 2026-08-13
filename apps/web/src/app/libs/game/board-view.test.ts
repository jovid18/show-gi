import { describe, expect, it } from 'vitest';

import type { Player, Snapshot, Status } from '@/protocol/game';

import { resultText } from './board-view';

/** 결과 문구는 `status` 와 `winner` 만 본다. 나머지는 안 읽으므로 최소한만 채운다. */
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
  };
  // 中断에는 승자가 아예 안 온다. `winner: undefined` 를 넣으면 「빈 승자」라는 없는 값이 생긴다.
  return winner ? { ...base, winner } : base;
}

describe('resultText', () => {
  it('中断は勝ち負けを言わない', () => {
    // 서버가 상대의 수를 못 구해서 접은 판이다. 「相手が投了しました」로 그리면 지고
    // 있던 판이 화면에서 이긴 판이 된다 — 사람이 둔 첫 판이 그 반대 방향으로 겪은 실패다.
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
