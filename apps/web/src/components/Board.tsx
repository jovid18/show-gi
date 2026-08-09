import type { CSSProperties } from 'react';

import { Koma } from './Koma';
import type { Player } from '@/game/protocol';
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
 * 방금 그 화면에서 벌어진 한 수를 판 위에 그은 선.
 *
 * **판은 언제나 이 수를 둔 뒤의 국면이다.** 그래서 이 선은 지금 화면에 대한 사실이다 —
 * 수순을 넘겨 보지 않고 한 판 위에 여러 수를 겹쳐 그으면 그 순간 거짓말이 된다.
 * 실제로 「상대가 아직 손에 없는 駒를 놓는 수」를 그리고 있었다.
 */
export interface Ray {
  /** 출발 칸. 打이면 null이고, 그때는 도착점만 찍힌다. */
  from: number | null;
  to: number;
  /** 누가 둔 수인가. 읽어야 하는 것은 상대의 수라 사람의 수는 물러난다. */
  by: Player;
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
  /** 물러진 수를 되짚는 유령 駒. 넘겨 보기의 첫 장면에서만 채워진다. */
  replay: Replay | null;
  /** 지금 화면의 한 수가 지나간 길. 넘겨 보는 동안 채워진다. */
  ray: Ray | null;
  /** 회상 중인가. 판이 색을 잃고 낮아져서 그 위의 빛이 읽힌다. */
  dimmed: boolean;
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
function RefutationRay({ ray, waitForGhost }: { ray: Ray; waitForGhost: boolean }) {
  const col = ray.to % BOARD_SIZE;
  const row = Math.floor(ray.to / BOARD_SIZE);

  // 打도 어디선가 온다 — **駒台에서 온다.** 도착점만 찍으면 「저기 뭔가 있다」까지이고,
  // 초심자가 알아야 하는 것은 그 駒가 반상에 없던 것이라는 사실이다. 그래서 판 밖,
  // 그 사람의 駒台 쪽에서 시작해 판으로 들어온다. 화면은 늘 相手가 위·あなた가 아래다.
  const drop = ray.from === null;
  const fromCol = ray.from === null ? col : ray.from % BOARD_SIZE;
  const fromRow = ray.from === null ? (ray.by === 'engine' ? -1 : BOARD_SIZE) : Math.floor(ray.from / BOARD_SIZE);

  const dcol = col - fromCol;
  const drow = row - fromRow;

  const style = {
    '--col': fromCol,
    '--row': fromRow,
    '--len': Math.hypot(dcol, drow),
    '--angle': `${(Math.atan2(drow, dcol) * 180) / Math.PI}deg`,
  } as CSSProperties;

  return (
    <span
      className="refutation-ray"
      data-by={ray.by}
      data-drop={drop || undefined}
      // 유령 駒가 나는 장면에서만 기다렸다 켜진다. 넘기며 보는 동안에는 기다릴 것이 없다.
      data-wait={waitForGhost || undefined}
      style={style}
      aria-hidden="true"
    />
  );
}

export function Board({
  board,
  lit,
  selected,
  lastMove,
  checked,
  replay,
  ray,
  dimmed,
  interactive,
  onSquare,
}: BoardProps) {
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
          const mark = ray?.from === index ? 'from' : ray?.to === index ? 'to' : null;
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

        {/* 판만 낮춘다. 빛과 광선은 이 겹 위에 있어서 낮아진 판 위에서 오히려 또렷해진다.
            판을 밝게 둔 채로는 흰 광선이 榧색 나무에 묻힌다 — D2에서 한 번 겪은 일이다. */}
        {dimmed && <span className="board-tint" aria-hidden="true" />}

        {ray && <RefutationRay ray={ray} waitForGhost={replay !== null} />}

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
