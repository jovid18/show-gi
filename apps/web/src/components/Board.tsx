import type { CSSProperties } from 'react';

import type { Board as BoardModel } from '@/shogi/sfen';
import { isWideKanji, kanjiOf, type Side } from '@/shogi/piece';
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
      <span className="koma" data-side={replay.side}>
        <span className="koma-kanji" data-wide={isWideKanji(replay.kind) || undefined}>
          {kanjiOf(replay.kind)}
        </span>
      </span>
    </span>
  );
}

export function Board({ board, lit, selected, lastMove, checked, replay, interactive, onSquare }: BoardProps) {
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
              aria-label={piece ? `${label} ${kanjiOf(piece.kind)}` : label}
              onClick={() => onSquare(usi)}
            >
              {piece && (
                <span className="koma" data-side={piece.side}>
                  <span className="koma-kanji" data-wide={isWideKanji(piece.kind) || undefined}>
                    {kanjiOf(piece.kind)}
                  </span>
                </span>
              )}
              {mark && <span className="blunder-mark" data-role={mark} aria-hidden="true" />}
            </button>
          );
        })}

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
