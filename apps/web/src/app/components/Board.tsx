import { useRef, useState, type CSSProperties, type RefObject } from 'react';

import { Koma } from './Koma';
import type { Player } from '@/protocol/game';
import type { Board as BoardModel } from '@/models/sfen';
import { nameOf, type Side } from '@/models/piece';
import { fromIndex, toUsi, type Motion } from '@/models/square';
import { useBoardSurface } from '@/hooks/useBoardSurface';

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
  /** 누가 둔 수인가. */
  by: Player;
  /** 王手를 거는 줄인가. 다음 수(초록)와 색만 다르고 모양은 같다. */
  check?: boolean;
  /**
   * 갇힘 힌트인가. 파랑으로 긋는다.
   *
   * 모양을 초록·빨강과 같이 두는 것은 「한 수를 잇는 선」이라는 뜻이 같기 때문이고,
   * 색을 가르는 것은 **가리키는 쪽이 반대**이기 때문이다. 저 둘은 상대가 무엇을 하는가,
   * 이쪽은 네가 무엇을 두면 되는가다. 같은 색으로 두면 판이 정반대를 같은 말로 한다.
   */
  hint?: boolean;
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
  /** 방금 이 판을 만든 수의 움직임. 도착 칸의 駒가 출발 칸에서 미끄러져 들어온다. */
  motion: Motion | null;
  /**
   * 지금 玉을 잡으러 오는 말들. 붉은 화살표로 긋는다.
   *
   * **몇 줄인지가 곧 답이다.** 둘이면 両王手라 玉을 움직일 수밖에 없고, 「먹어서 풀면
   * 되지 않나」가 거기서 닫힌다 — 실제로 그 물음이 나왔다.
   */
  checks: readonly Ray[];
  /** 회상 중인가. 판이 색을 잃고 낮아져서 그 위의 빛이 읽힌다. */
  dimmed: boolean;
  /** 打 화살표의 출발점. 재기 전이거나 打이 아니면 null. */
  dropFrom: DropFrom | null;
  /** 갇힘 힌트가 짚는 칸. 파란 테를 두른다. 打이거나 아직 안 열렸으면 null. */
  hintSquare: string | null;
  /** 갇힘 힌트의 마지막 단계 — 그 수 자체. 파란 화살표로 긋는다. */
  hintRay: Ray | null;
  /**
   * 詰み 게이지의 세기(1~5). 0이면 안 그린다.
   *
   * 판 **테두리**에 보라 불꽃으로 붙는다. 판 안(칸·기물)은 「강조는 색이 아니라 빛」인데
   * 테두리는 다른 표면이라 색을 써도 그 체계를 흐리지 않는다(docs/01-core.md §7).
   *
   * **회상 중에는 0으로 받는다.** 그때 판은 물러진 수의 국면이라, 지금 국면의 게이지를
   * 거기에 얹으면 그 순간 거짓말이 된다 — 광선을 한 판 위에 겹쳐 긋지 않는 것과 같은 이유다.
   */
  mateHeat: number;
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
        data-hint={ray.hint || undefined}
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
      data-check={ray.check || undefined}
      data-hint={ray.hint || undefined}
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
  motion,
  checks,
  dimmed,
  dropFrom,
  hintSquare,
  hintRay,
  mateHeat,
  boardRef,
  interactive,
  onSquare,
}: BoardProps) {
  /**
   * 판을 three.js가 그리는가. **판을 재는 쪽이 ref를 잡고 있으면 그걸 같이 쓴다** —
   * 여기서 두 번째 ref를 붙이면 표면이 판이 아니라 아무것도 안 붙은 요소를 잰다.
   */
  const ownRef = useRef<HTMLDivElement>(null);
  const surfaceRef = boardRef ?? ownRef;

  /**
   * 그늘을 켜 두었는가.
   *
   * **회상 중에는 물어보지 않고 켠다.** 그 자리가 「네가 두려던 칸이 왜 위험했나」이고,
   * 판 위에서 글 없이 그 답을 하는 것이 이 표면이 있는 이유다(docs/03-frontend.md §1).
   */
  const [showExposure, setShowExposure] = useState(false);
  /**
   * 그늘을 지금 그리나. **회상 중에도 사람이 켠 때만이다.**
   *
   * 한때 `|| dimmed` 가 붙어 있어서 물러진 수를 되짚는 동안 그늘이 강제로 켜지고 토글이
   * 잠겼다. 그런데 탈색된 판 위의 그 둥근 얼룩은 「상대가 손을 뻗은 칸」으로 안 읽히고
   * **판에 낀 흠**으로 읽힌다 — 그 자리에서 말해야 하는 것은 「이 수를 물렀다」 하나이고,
   * 그건 테와 화살표가 이미 말한다.
   *
   * 켜고 싶으면 회상 중에도 켤 수 있다(토글을 안 잠근다).
   */
  const exposed = showExposure;

  const ready = useBoardSurface({
    boardRef: surfaceRef,
    board,
    active: exposed,
    // 회상에서는 **물러진 수가 간 칸**에서 그늘이 퍼진다. 평시에는 판 한가운데다.
    from: dimmed ? (played?.to ?? null) : null,
  });

  return (
    <div className="board-frame">
      <div className="board-files" aria-hidden="true">
        {FILES.map((f) => (
          <span key={f}>{f}</span>
        ))}
      </div>

      <div className="board" ref={surfaceRef} data-surface={ready || undefined}>
        {board.squares.map((piece, index) => {
          const usi = toUsi(fromIndex(index));
          const label = `${FILES[index % BOARD_SIZE]}${RANKS[Math.floor(index / BOARD_SIZE)]}`;
          // 물러진 수가 지나간 두 칸. **도착 칸을 빼면 안 된다** — 打은 출발 칸이 없어서
          // 화살표가 아예 안 나가고(ReviewDetail 의 `retracted`), 그때 도착 칸 표식이
          // 「어디에 놓으려 했나」를 짚는 유일한 것이다. 한 칸이 둘을 겸하는 수는 없다.
          const mark = played?.from === index ? 'from' : played?.to === index ? 'to' : null;
          const last = lastMove?.to === index ? 'to' : lastMove?.from === index ? 'from' : undefined;

          // 이 칸으로 駒가 들어오는 중인가. **출발 칸에서 도착 칸까지를 칸 수로 준다** —
          // 픽셀로 계산하면 `--sq` 가 화면 폭을 따라 변하는 만큼 어긋난다(유령 駒와 같다).
          const moving = motion?.to === index ? motion : null;
          const slide =
            moving && moving.from !== null
              ? ({
                  '--mcol': (moving.from % BOARD_SIZE) - (index % BOARD_SIZE),
                  '--mrow': Math.floor(moving.from / BOARD_SIZE) - Math.floor(index / BOARD_SIZE),
                } as CSSProperties)
              : undefined;

          return (
            <button
              // 같은 칸에 연달아 들어오면(되잡기) 요소가 그대로 남아 애니메이션이 다시
              // 시작하지 않는다. 열쇠를 바꿔 그 칸만 새로 붙인다.
              key={moving ? `${usi}-${moving.id}` : usi}
              type="button"
              className="square"
              style={slide}
              data-motion={moving ? (moving.from === null ? 'drop' : 'board') : undefined}
              data-lit={lit.has(usi) || undefined}
              data-occupied={piece ? true : undefined}
              data-selected={selected === usi || undefined}
              data-last={last}
              data-check={checked === usi || undefined}
              disabled={!interactive}
              aria-label={piece ? `${label} ${nameOf(piece.kind)}` : label}
              onClick={() => onSquare(usi)}
            >
              {/* 駒보다 먼저 그린다. 뒤에 오는 .koma 가 position:relative 라 그 위에 얹힌다 */}
              {hintSquare === usi && <span className="hint-outline" aria-hidden="true" />}
              {piece && <Koma kind={piece.kind} side={piece.side} />}
              {mark && <span className="blunder-mark" data-role={mark} aria-hidden="true" />}
            </button>
          );
        })}

        {/* 판만 낮춘다. 빛과 광선은 이 겹 위에 있어서 낮아진 판 위에서 오히려 또렷해진다.
            판을 밝게 둔 채로는 흰 광선이 榧색 나무에 묻힌다 — D2에서 한 번 겪은 일이다. */}
        {dimmed && <span className="board-tint" aria-hidden="true" />}

        {/* 王手가 먼저 켜진다. 「지금 이 판이 어떤 상태인가」가 「다음에 무엇이 오는가」보다
            앞이다 — 王手인 줄 모르면 다음 수가 왜 그것인지도 안 읽힌다. */}
        {checks.map((c) => (
          <RefutationRay key={`${c.from}-${c.to}`} ray={c} waitForGhost={false} dropFrom={null} />
        ))}

        {ray && <RefutationRay ray={ray} waitForGhost={replay !== null} dropFrom={dropFrom} />}

        {/* 힌트는 마지막에 켠다. 상대 쪽 광선과 겹치는 국면에서 가려지면 안 되는 쪽이 이쪽이다 */}
        {hintRay && <RefutationRay ray={hintRay} waitForGhost={false} dropFrom={dropFrom} />}

        {replay && <ReplayKoma replay={replay} />}

        {/* 게이지는 판 밖으로 번진다(inset 이 음수라 테두리 위에 얹힌다). 그래서 판 안의
            어느 것과도 자리를 다투지 않고, 마지막에 그려도 무엇을 가리지 않는다. */}
        {mateHeat > 0 && (
          <span className="mate-flame" style={{ '--heat': mateHeat } as CSSProperties} aria-hidden="true" />
        )}
      </div>

      <div className="board-ranks" aria-hidden="true">
        {RANKS.map((r) => (
          <span key={r}>{r}</span>
        ))}
      </div>

      {/* WebGL이 안 잡히면 아예 안 내놓는다. 눌러도 아무 일이 안 일어나는 버튼은
          「고장 났다」로 읽힌다. **회상 중에도 잠그지 않는다** — 그늘이 강제로 켜지지
          않으므로(위 `exposed`) 끄지 못하게 할 이유가 없다. */}
      {ready && (
        <button
          type="button"
          className="exposure-toggle"
          aria-pressed={exposed}
          title="相手の駒が利いていて、こちらが受けていないマスに影が落ちる"
          onClick={() => setShowExposure((on) => !on)}
        >
          相手の利き
        </button>
      )}
    </div>
  );
}
