import type { CSSProperties } from 'react';

import { Koma } from './Koma';
import type { Board as BoardModel } from '@/shogi/sfen';
import { nameOf, type Side } from '@/shogi/piece';
import { fromIndex, toUsi } from '@/shogi/square';

const FILES = [9, 8, 7, 6, 5, 4, 3, 2, 1];
const RANKS = ['一', '二', '三', '四', '五', '六', '七', '八', '九'];
const BOARD_SIZE = 9;

/**
 * 물러진 수를 판 위에서 되짚기 위한 것. 칸은 **화면 배열 인덱스**(0~80)로 받는다 —
 * 좌표 문자열을 여기서 다시 풀면 못 읽는 값에 판이 통째로 안 그려질 수 있다.
 */
export interface Replay {
  /** 출발 칸. 持ち駒를 둔 수(打)면 null. */
  from: number | null;
  to: number;
  /** 움직인 기물의 종류. 승격 표기를 포함한다(`+B`). */
  kind: string;
  side: Side;
}

/** 직전 수가 지나간 두 칸. 화면 배열 인덱스이고, 打이면 `from` 이 null. */
export interface LastMove {
  from: number | null;
  to: number;
}

/**
 * 상대가 그 수를 벌하는 **한 수**를 판 위에 그은 선. 반박 수순의 첫 수다.
 *
 * **수순 전체를 긋지 않는다.** 판에 그릴 수 있는 것은 지금 판이 사실인 수뿐인데, 그 조건을
 * 만족하는 것은 첫 수 하나다 — 두 번째 상대 수부터는 사이에 오지 않을 응수를 전제하고,
 * 실제로 「아직 손에 없는 駒를 놓는 수」가 나온다. 그것을 지금 판 위에 그리면 연출이
 * 아니라 국면에 대한 거짓말이 된다. 수순 전체는 옆의 문구가 棋譜로 말한다.
 *
 * **사람의 수도 긋지 않는다.** 지금 판에 서 있는 내 駒에서 뻗는 광선은 문구가 무엇이라고
 * 적혀 있든 「이렇게 두라」로 읽힌다. 그 선은 이 제품이 긋지 않기로 한 자리다
 * (docs/01-core.md §1).
 */
export interface Ray {
  /** 출발 칸. 打이면 null이고, 그때는 도착점만 찍힌다. */
  from: number | null;
  to: number;
}

interface BoardProps {
  board: BoardModel;
  /** 지금 빛나는 칸(착수 가능). USI 좌표. */
  lit: ReadonlySet<string>;
  /** 고른 기물이 서 있는 칸. */
  selected: string | null;
  /** 직전 수. 출발 칸과 도착 칸을 함께 짚는다. */
  lastMove: LastMove | null;
  /** 王手를 받고 있는 玉의 칸. */
  checked: string | null;
  /** 개입 연출 동안만 채워진다. */
  replay: Replay | null;
  /** 상대가 그 수를 어떻게 벌하는가. 개입 연출 동안만 채워진다. */
  ray: Ray | null;
  interactive: boolean;
  onSquare: (usi: string) => void;
}

/**
 * 물러진 수를 한 번 재생하는 유령 駒.
 *
 * 자리도 이동 거리도 **칸 수**로 준다(`--col`·`--row`·`--dcol`·`--drow`). 픽셀로 계산하면
 * `--sq` 가 화면 폭에 따라 변하는 만큼 어긋난다.
 *
 * **격자에 얹지 않고 띄운다.** 자리를 `grid-column`으로 지정하면 그 칸이 먼저 잡히고
 * 81칸이 그것을 피해 한 칸씩 밀린다 — 명시 배치는 DOM 순서보다 먼저 처리된다.
 */
function ReplayKoma({ replay }: { replay: Replay }) {
  const start = replay.from ?? replay.to;
  const style = {
    '--col': start % BOARD_SIZE,
    '--row': Math.floor(start / BOARD_SIZE),
    '--dcol': (replay.to % BOARD_SIZE) - (start % BOARD_SIZE),
    '--drow': Math.floor(replay.to / BOARD_SIZE) - Math.floor(start / BOARD_SIZE),
  } as CSSProperties;

  return (
    <span className="replay-koma" data-drop={replay.from === null || undefined} style={style} aria-hidden="true">
      <Koma kind={replay.kind} side={replay.side} />
    </span>
  );
}

/**
 * 상대의 벌하는 수를 칸 중심에서 칸 중심으로 잇는 광선.
 *
 * **길이와 각도는 여기서 계산해 CSS로 넘긴다.** `sqrt()`·`atan2()` 는 브라우저마다 언제
 * 들어왔는지가 갈리는데, 판이 안 그려지는 대가로 얻을 것이 없다. 자리는 유령 駒와 같이
 * **칸 수**로 준다 — 픽셀로 주면 `--sq` 가 화면 폭을 따라 변하는 만큼 어긋난다.
 */
function RefutationRay({ ray }: { ray: Ray }) {
  // 打은 출발 칸이 없다. 길이 0으로 두면 도착점만 남는다 — 그것이 打의 사실 그대로다.
  const start = ray.from ?? ray.to;
  const dcol = ray.from === null ? 0 : (ray.to % BOARD_SIZE) - (start % BOARD_SIZE);
  const drow = ray.from === null ? 0 : Math.floor(ray.to / BOARD_SIZE) - Math.floor(start / BOARD_SIZE);

  const style = {
    '--col': start % BOARD_SIZE,
    '--row': Math.floor(start / BOARD_SIZE),
    '--len': Math.hypot(dcol, drow),
    '--angle': `${(Math.atan2(drow, dcol) * 180) / Math.PI}deg`,
  } as CSSProperties;

  return <span className="refutation-ray" style={style} aria-hidden="true" />;
}

export function Board({ board, lit, selected, lastMove, checked, replay, ray, interactive, onSquare }: BoardProps) {
  return (
    <div className="board-frame">
      <div className="board-files" aria-hidden="true">
        {FILES.map((f) => (
          <span key={f}>{f}</span>
        ))}
      </div>

      <div className="board">
        {board.squares.map((piece, index) => {
          const usi = toUsi(fromIndex(index));
          const label = `${FILES[index % BOARD_SIZE]}${RANKS[Math.floor(index / BOARD_SIZE)]}`;
          // 한 칸이 出発と到着을 겸하는 수는 없다. 겸치면 그릴 것도 없다.
          const mark = replay?.from === index ? 'from' : replay?.to === index ? 'to' : null;
          const last = lastMove?.to === index ? 'to' : lastMove?.from === index ? 'from' : undefined;

          return (
            <button
              key={usi}
              type="button"
              className="square"
              data-lit={lit.has(usi) || undefined}
              data-occupied={piece ? true : undefined}
              data-selected={selected === usi || undefined}
              data-last={last}
              data-check={checked === usi || undefined}
              disabled={!interactive}
              aria-label={piece ? `${label} ${nameOf(piece.kind)}` : label}
              onClick={() => onSquare(usi)}
            >
              {piece && <Koma kind={piece.kind} side={piece.side} />}
              {mark && <span className="blunder-mark" data-role={mark} aria-hidden="true" />}
            </button>
          );
        })}

        {ray && <RefutationRay ray={ray} />}

        {replay && <ReplayKoma replay={replay} />}
      </div>

      <div className="board-ranks" aria-hidden="true">
        {RANKS.map((r) => (
          <span key={r}>{r}</span>
        ))}
      </div>
    </div>
  );
}
