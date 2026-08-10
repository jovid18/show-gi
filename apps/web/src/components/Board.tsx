import type { CSSProperties, RefObject } from 'react';

import { Koma } from './Koma';
import type { Player } from '@/game/protocol';
import type { Board as BoardModel } from '@/shogi/sfen';
import { nameOf, type Side } from '@/shogi/piece';
import { fromIndex, toUsi } from '@/shogi/square';

const FILES = [9, 8, 7, 6, 5, 4, 3, 2, 1];
const RANKS = ['一', '二', '三', '四', '五', '六', '七', '八', '九'];
const BOARD_SIZE = 9;

/**
 * 打 화살표의 출발점 — **駒台에 놓인 그 駒의 실제 자리**다. 판의 안쪽 모서리를 기준으로
 * 한 px이고, `--sq` 는 그때 재어 둔 칸 크기다.
 *
 * 칸 산수로는 안 나온다. 駒台는 판 밖의 형제 요소이고 그 안에서 持ち駒가 몇 종류인지·
 * 라벨이 얼마나 넓은지에 따라 자리가 달라진다 — 그래서 **재야** 하고, 재면 된다.
 */
export interface DropFrom {
  x: number;
  y: number;
  sq: number;
}

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
  /** 지금 판을 만든 수. 흰빛 두 칸으로 짚는다 — **방금 벌어진 것**이다. */
  played: LastMove | null;
  /** 다음에 올 한 수. 초록 화살표로 긋는다 — **다음에 벌어질 것**이다. */
  ray: Ray | null;
  /** 회상 중인가. 판이 색을 잃고 낮아져서 그 위의 빛이 읽힌다. */
  dimmed: boolean;
  /** 打 화살표의 출발점. 재기 전이거나 打이 아니면 null. */
  dropFrom: DropFrom | null;
  /** 판 요소. 駒台와의 거리를 재는 쪽이 잡는다. */
  boardRef?: RefObject<HTMLDivElement | null>;
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
function RefutationRay({
  ray,
  waitForGhost,
  dropFrom,
}: {
  ray: Ray;
  waitForGhost: boolean;
  dropFrom: DropFrom | null;
}) {
  const col = ray.to % BOARD_SIZE;
  const row = Math.floor(ray.to / BOARD_SIZE);
  const drop = ray.from === null;

  // 打은 **판 위에 출발 칸이 없다.** 駒台에 놓인 그 駒에서 출발해야 「어느 駒가 나가는가」가
  // 읽히는데, 그 자리는 칸 산수 밖이라 재어서 받는다. 아직 못 쟀으면 안 긋는다 —
  // 엉뚱한 자리에서 뻗는 화살표는 없는 데서 駒를 가져오는 것으로 보인다.
  if (drop) {
    if (!dropFrom) return null;
    const { x, y, sq } = dropFrom;
    // 1px 은 .board 의 padding 이자 칸 사이 gap 이다.
    const dx = 1 + col * (sq + 1) + sq / 2 - x;
    const dy = 1 + row * (sq + 1) + sq / 2 - y;
    const style = {
      '--x': `${x}px`,
      '--y': `${y}px`,
      '--len-px': `${Math.hypot(dx, dy)}px`,
      '--angle': `${(Math.atan2(dy, dx) * 180) / Math.PI}deg`,
    } as CSSProperties;

    return (
      <span
        className="refutation-ray"
        data-by={ray.by}
        data-anchored
        data-wait={waitForGhost || undefined}
        style={style}
        aria-hidden="true"
      />
    );
  }

  const fromCol = ray.from! % BOARD_SIZE;
  const fromRow = Math.floor(ray.from! / BOARD_SIZE);
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
  played,
  ray,
  dimmed,
  dropFrom,
  boardRef,
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

      <div className="board" ref={boardRef}>
        {board.squares.map((piece, index) => {
          const usi = toUsi(fromIndex(index));
          const label = `${FILES[index % BOARD_SIZE]}${RANKS[Math.floor(index / BOARD_SIZE)]}`;
          // 한 칸이 出発と到着을 겸하는 수는 없다. 겸치면 그릴 것도 없다.
          const mark = played?.from === index ? 'from' : played?.to === index ? 'to' : null;
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

        {ray && <RefutationRay ray={ray} waitForGhost={replay !== null} dropFrom={dropFrom} />}

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
