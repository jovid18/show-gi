import { describe, expect, it } from 'vitest';

import { exposure, influenceOf } from './influence';
import { parseSfen } from './sfen';
import { toIndex, fromUsi } from './square';

/** `'5e'` 칸의 매수. 좌표를 손으로 인덱스로 옮기면 그 자리에서 읽을 수 없게 된다. */
const at = (count: Uint8Array, usi: string) => count[toIndex(fromUsi(usi))];

const lit = (count: Uint8Array) => new Set(Array.from(count, (n, i) => (n > 0 ? i : -1)).filter((i) => i >= 0));

/** 판만 있는 SFEN. 持ち駒는 利き과 무관하다 — 打은 駒가 판에 놓인 뒤의 일이다. */
const board = (rows: string) => parseSfen(`${rows} b -`);

describe('influenceOf', () => {
  it('歩는 한 칸 앞만 — 先手는 위로, 後手는 아래로', () => {
    const black = influenceOf(board('9/9/9/9/4P4/9/9/9/9')).black;
    expect(lit(black)).toEqual(new Set([toIndex(fromUsi('5d'))]));

    const white = influenceOf(board('9/9/9/9/4p4/9/9/9/9')).white;
    expect(lit(white)).toEqual(new Set([toIndex(fromUsi('5f'))]));
  });

  it('쭉 가는 駒는 처음 만난 駒에서 멈추고 **그 칸까지 센다**', () => {
    // 5九香 위로. 5六에 상대 歩가 막고 서 있다 — 자기 歩로 막으면 그 歩의 利き이
    // 5五에 얹혀서 「香가 뚫고 갔는가」를 이 자리에서 못 가른다.
    const { black } = influenceOf(board('9/9/9/9/9/4p4/9/9/4L4'));
    expect(at(black, '5h')).toBe(1);
    expect(at(black, '5g')).toBe(1);
    // 막고 선 칸은 잡을 수 있는 자리이므로 센다.
    expect(at(black, '5f')).toBe(1);
    // 그 너머는 없다. 세면 판이 없는 위협을 말한다.
    expect(at(black, '5e')).toBe(0);
  });

  it('桂는 사이 칸을 밟지 않는다 — 두 칸 앞의 좌우뿐이다', () => {
    const { black } = influenceOf(board('9/9/9/9/9/9/9/4N4/9'));
    expect(lit(black)).toEqual(new Set([toIndex(fromUsi('4f')), toIndex(fromUsi('6f'))]));
    // 대각선 한 칸은 銀의 그림이다. 여기 세면 판이 규칙을 틀리게 가르친다.
    expect(at(black, '4g')).toBe(0);
    expect(at(black, '6g')).toBe(0);
  });

  it('後手 駒는 통째로 돌아 있다 — 桂가 거울상이 되지 않는다', () => {
    const { white } = influenceOf(board('9/4n4/9/9/9/9/9/9/9'));
    expect(lit(white)).toEqual(new Set([toIndex(fromUsi('4d')), toIndex(fromUsi('6d'))]));
  });

  it('여러 駒가 같은 칸에 닿으면 매수가 쌓인다', () => {
    // 5五에 金(5六)과 飛(5九, 사이가 비어 있다)가 함께 닿는다.
    const { black } = influenceOf(board('9/9/9/9/9/4G4/9/9/4R4'));
    expect(at(black, '5e')).toBe(1); // 金만 — 飛는 5六의 金에서 멈춘다
    expect(at(black, '5f')).toBe(1); // 飛가 막고 선 자기 金을 받치고 있다
    expect(at(black, '4f')).toBe(1); // 金의 옆
  });

  it('양쪽이 따로 세어진다', () => {
    const { black, white } = influenceOf(board('9/9/9/4p4/9/4P4/9/9/9'));
    expect(at(black, '5e')).toBe(1);
    expect(at(white, '5e')).toBe(1);
  });

  it('초기 국면에서 玉의 앞은 비어 있지 않다', () => {
    const { black } = influenceOf(parseSfen('lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b -'));
    // 歩가 늘어선 줄의 앞(6段)은 전부 한 매씩 닿는다.
    for (const file of [1, 2, 3, 4, 5, 6, 7, 8, 9]) {
      expect(at(black, `${file}f`)).toBe(1);
    }
  });
});

describe('exposure', () => {
  it('받고 있는 만큼 뺀다 — 음수는 0이다', () => {
    // 5五를 後手 歩(5四)가 겨누고, 先手 金(5六)이 받는다. 매수가 같으면 **어느 쪽에서
    // 봐도 그늘이 없다** — 뺄셈 하나로 두 방향이 같이 닫힌다.
    const influence = influenceOf(board('9/9/9/4p4/9/4G4/9/9/9'));
    expect(exposure(influence, 'black')[toIndex(fromUsi('5e'))]).toBe(0);
    expect(exposure(influence, 'white')[toIndex(fromUsi('5e'))]).toBe(0);
  });

  it('한 매가 더 붙으면 그만큼만 깊어진다', () => {
    // 5五에 後手 飛(5一)와 後手 角(3三)이 닿고, 先手 金(5六)이 한 매로 받는다.
    // 「두 매로 오는데 한 매로 받는다」가 그늘이 깊어지는 자리다.
    const influence = influenceOf(board('4r4/9/6b2/9/9/4G4/9/9/9'));
    expect(influence.white[toIndex(fromUsi('5e'))]).toBe(2);
    expect(influence.black[toIndex(fromUsi('5e'))]).toBe(1);
    expect(exposure(influence, 'black')[toIndex(fromUsi('5e'))]).toBe(1);
  });

  it('아무도 안 받는 칸이 깊다', () => {
    // 後手 飛(5一)가 5五까지 내려온다. 받는 駒가 없다.
    const influence = influenceOf(board('4r4/9/9/9/9/9/9/9/9'));
    expect(exposure(influence, 'black')[toIndex(fromUsi('5e'))]).toBe(1);
  });
});
