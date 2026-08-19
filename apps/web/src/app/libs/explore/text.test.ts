import { describe, expect, it } from 'vitest';

import { baselineNoteJa, exploreStatusJa, sideJa } from './text';
import type { ExploreNode } from '@/protocol/explore';

/** 문구가 보는 칸만 채운 노드. 나머지는 이 파일이 안 읽는다. */
function nodeOf(patch: Partial<ExploreNode>): ExploreNode {
  return {
    basePly: 0,
    ply: 0,
    sfen: '',
    turn: 'b',
    yourTurn: true,
    status: 'playing',
    legalMoves: [],
    line: [],
    candidates: [],
    ...patch,
  };
}

// 手合割의 말과 平手의 말이 다르다. 섞이면 접지도 않은 판에 상하가 생기거나, 駒落ち가
// 정석서와 다른 이름으로 불린다.
describe('sideJa', () => {
  it('平手는 先手·後手', () => {
    expect(sideJa('b', false)).toBe('先手');
    expect(sideJa('w', false)).toBe('後手');
  });

  it('駒落ち는 下手·上手', () => {
    expect(sideJa('b', true)).toBe('下手');
    expect(sideJa('w', true)).toBe('上手');
  });
});

describe('exploreStatusJa', () => {
  // 「あなた」가 없다. 검토에는 플레이어가 없고 양쪽 다 사람이 둔다.
  it('두는 중이면 手番을 말하고 양쪽을 둘 수 있다고 말한다', () => {
    expect(exploreStatusJa(nodeOf({ turn: 'w' }), false)).toBe('後手の番。どちらの駒も動かせます。');
    expect(exploreStatusJa(nodeOf({ turn: 'w', handicapJa: '二枚落ち' }), false)).toBe(
      '上手の番。どちらの駒も動かせます。',
    );
  });

  // 詰み은 수번 쪽이 지는 것이다.
  it('詰み·手詰まり는 수번 쪽의 패배다', () => {
    expect(exploreStatusJa(nodeOf({ turn: 'b', status: 'checkmate' }), false)).toBe('詰みです。先手の負けです。');
    expect(exploreStatusJa(nodeOf({ turn: 'w', status: 'stalemate' }), false)).toBe('手詰まりです。後手の負けです。');
  });

  it('아직 아무것도 못 받았으면 무엇을 하면 되는지를 말한다', () => {
    expect(exploreStatusJa(null, false)).toContain('盤の上で');
    expect(exploreStatusJa(null, true)).toBe('読んでいます…');
  });

  // 직전 국면을 그리고 있는 동안 문구를 바꾸지 않는다. 手合割을 바꾸거나 한 수 둘 때마다
  // 문구가 「読んでいます…」로 번쩍이면 그것만 눈에 남는다(WhatIfPanel 에서 배운 것이다).
  it('노드가 있으면 기다리는 동안에도 그 국면을 말한다', () => {
    expect(exploreStatusJa(nodeOf({ turn: 'b' }), true)).toBe('先手の番。どちらの駒も動かせます。');
  });
});

describe('baselineNoteJa', () => {
  it('駒落ち는 기준선을 말한다', () => {
    expect(baselineNoteJa(nodeOf({ handicapJa: '二枚落ち', baselineCp: 1386 }))).toBe(
      '二枚落ちの互角は +1386 あたりです。',
    );
  });

  // 平手는 기준점이 0이라 서버가 두 칸을 안 보낸다. 그 자리에 줄을 만들면 「互角は +0」이라는
  // 아무 말도 아닌 문장이 뜬다.
  it('平手는 아무 말도 하지 않는다', () => {
    expect(baselineNoteJa(nodeOf({}))).toBe('');
    expect(baselineNoteJa(null)).toBe('');
  });
});
