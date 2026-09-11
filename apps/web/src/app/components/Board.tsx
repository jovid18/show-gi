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
 * 打 화살표의 출발점. 駒台에 놓인 그 駒의 실제 자리이고, 판 안쪽 모서리 기준 px 이다.
 * `--sq` 는 그때 재어 둔 칸 크기다.
 *
 * 칸 산수로는 나오지 않아서 잰다. 駒台는 판 밖의 형제 요소이고, 持ち駒 종류 수와 라벨 폭에
 * 따라 자리가 달라진다.
 */
export interface DropFrom {
  x: number;
  y: number;
  sq: number;
}

/**
 * 물러진 수를 판 위에서 되짚기 위한 것. 칸은 화면 배열 인덱스(0~80)다. 좌표 문자열을
 * 여기서 다시 풀면 읽을 수 없는 값 하나에 판 전체가 그려지지 않을 수 있다.
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
 * 판은 언제나 이 수를 둔 뒤의 국면이다. 한 판 위에 여러 수를 겹쳐 그으면 그 자리에서
 * 판이 거짓을 말한다(journal §20).
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
   * 모양은 초록·빨강과 같다(「한 수를 잇는 선」으로 뜻이 같다). 색이 갈리는 것은 가리키는
   * 쪽이 반대여서다. 저 둘은 상대가 무엇을 하는가, 이쪽은 네가 무엇을 두면 되는가다.
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
  /** 지금 판을 만든 수. 흰빛 두 칸으로 짚는다. 방금 벌어진 것이다. */
  played: LastMove | null;
  /** 다음에 올 한 수. 초록 화살표로 긋는다. 다음에 벌어질 것이다. */
  ray: Ray | null;
  /** 방금 이 판을 만든 수의 움직임. 도착 칸의 駒가 출발 칸에서 미끄러져 들어온다. */
  motion: Motion | null;
  /**
   * 지금 玉을 잡으러 오는 말들. 붉은 화살표로 긋는다.
   *
   * 몇 줄인지가 곧 답이다. 둘이면 両王手라 「먹어서 풀면 되지 않나」가 거기서 닫힌다
   * (journal §20).
   */
  checks: readonly Ray[];
  /** 회상 중인가. 판이 색을 잃고 낮아져서 그 위의 빛이 읽힌다. */
  dimmed: boolean;
  /** 打 화살표의 출발점. 재기 전이거나 打이 아니면 null. */
  dropFrom: DropFrom | null;
  /** 갇힘 힌트가 짚는 칸. 파란 테를 두른다. 打이거나 아직 열리지 않았으면 null. */
  hintSquare: string | null;
  /** 갇힘 힌트의 마지막 단계, 곧 그 수 자체. 파란 화살표로 긋는다. */
  hintRay: Ray | null;
  /**
   * 詰み 게이지의 세기(1~5). 0이면 그리지 않는다.
   *
   * 판 테두리에 보라 불꽃으로 붙는다. 회상 중에는 0으로 받는다(journal §31).
   */
  mateHeat: number;
  /** 사람이 잡은 쪽. 그늘(`相手の利き`)이 누구 기준인지가 여기서 정해진다. */
  me: Side;
  /**
   * 판을 뒤집어 그리는가. 사람이 後手면 참이고, 그때 자기 駒가 아래에 온다.
   *
   * CSS `transform` 으로 돌리지 않는다. 돌리면 판 위의 자리를 재는 쪽이 전부 어긋난다.
   * 打 화살표의 출발점이 변형 전의 배치 좌표를 재고 있다(`useDropAnchor`). 칸의 자리
   * 번호만 뒤집으면 배치가 그대로여서 재는 값이 계속 맞는다.
   */
  flipped: boolean;
  /** 판 요소. 駒台와의 거리를 재는 쪽이 잡는다. */
  boardRef?: RefObject<HTMLDivElement | null>;
  interactive: boolean;
  /**
   * 착수음 스위치. 없으면 버튼이 나오지 않는다. 되짚기에는 착수가 없어서 켤 것이 없다.
   */
  sound?: { on: boolean; toggle: () => void };
  /**
   * 판을 뒤집는 스위치. 없으면 버튼이 나오지 않는다. 그 손잡이를 판 밖에 두는 화면이 아직
   * 있다(ReviewDetail). 착수음·그늘과 한 줄에 서는 이유는 journal §96.
   */
  flip?: { on: boolean; toggle: () => void };
  onSquare: (usi: string) => void;
}

/**
 * 물러진 수가 어느 駒였는지를 짚는 유령 駒. 가려던 칸에 서고 끌고 오지 않는다
 * (journal §69).
 *
 * 자리는 칸 수로 준다(`--col`·`--row`). 픽셀로 계산하면 `--sq` 가 화면 폭에 따라 변하는
 * 만큼 어긋난다.
 *
 * 격자에 얹지 않고 띄운다. `grid-column` 으로 자리를 주면 81칸이 그 칸을 피해 한 칸씩
 * 밀린다(journal §14).
 */
function ReplayKoma({ replay }: { replay: Replay }) {
  const style = {
    '--col': replay.to % BOARD_SIZE,
    '--row': Math.floor(replay.to / BOARD_SIZE),
  } as CSSProperties;

  return (
    <span className="replay-koma" style={style} aria-hidden="true">
      <Koma kind={replay.kind} side={replay.side} />
    </span>
  );
}

/**
 * 상대의 벌하는 수를 칸 중심에서 칸 중심으로 잇는 광선. 자리는 유령 駒와 같이 칸 수로 준다.
 *
 * 길이와 각도는 여기서 계산해 CSS 로 넘긴다. CSS 의 `sqrt()`·`atan2()` 는 브라우저마다
 * 들어온 시기가 갈리고, 판이 그려지지 않는 대가로 얻을 것이 없다.
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

  // 打은 판 위에 출발 칸이 없다. 駒台에 놓인 그 駒에서 출발해야 「어느 駒가 나가는가」가
  // 읽히고, 그 자리는 칸 산수 밖이라 재어서 받는다. 아직 재지 못했으면 긋지 않는다.
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
  me,
  flipped,
  boardRef,
  interactive,
  sound,
  flip,
  onSquare,
}: BoardProps) {
  /**
   * 판 배열 인덱스 ↔ 화면 자리 번호. 뒤집으면 180° 돌린 자리이고, 두 번 걸면 제자리라
   * 양방향에 같은 함수를 쓴다.
   *
   * 좌표 라벨에는 걸지 않는다. `7六` 은 판의 절대 좌표라 누가 어느 쪽에 앉아도 같다.
   */
  const seat = (i: number): number => (flipped ? 80 - i : i);
  const seatRay = (r: Ray): Ray =>
    flipped ? { ...r, from: r.from === null ? null : seat(r.from), to: seat(r.to) } : r;
  // 판을 재는 쪽이 ref 를 잡고 있으면 그걸 같이 쓴다. 두 번째 ref 를 붙이면 three.js 표면이
  // 아무것도 붙지 않은 요소를 잰다.
  const ownRef = useRef<HTMLDivElement>(null);
  const surfaceRef = boardRef ?? ownRef;

  /** 그늘을 켜 두었는가. 사람이 토글로 켤 때만 참이다. */
  const [showExposure, setShowExposure] = useState(false);
  /**
   * 그늘을 지금 그리나. 회상 중이라고 강제로 켜지지 않는다.
   *
   * 탈색된 판 위의 둥근 얼룩은 「상대가 손을 뻗은 칸」으로 읽히지 않고 판에 낀 흠으로
   * 읽힌다. 그 자리에서 말해야 하는 「이 수를 물렀다」는 테와 화살표가 이미 말한다.
   * 사람이 켜고 싶으면 회상 중에도 켤 수 있다.
   */
  const exposed = showExposure;

  const ready = useBoardSurface({
    boardRef: surfaceRef,
    board,
    active: exposed,
    // 회상에서는 물러진 수가 간 칸에서 그늘이 퍼진다. 평시에는 판 한가운데다.
    from: dimmed ? (played?.to ?? null) : null,
    me,
    flipped,
  });

  return (
    <div className="board-frame">
      {/* 좌표는 판의 절대 좌표다. 뒤집힌 판에서는 줄만 거꾸로 늘어놓는다. 1筋이 오른쪽인
          것은 先手 기준이고, 後手에게는 그 반대가 자기 오른쪽이다. */}
      <div className="board-files" aria-hidden="true">
        {(flipped ? FILES.toReversed() : FILES).map((f) => (
          <span key={f}>{f}</span>
        ))}
      </div>

      <div className="board" ref={surfaceRef} data-surface={ready || undefined}>
        {board.squares.map((_, spot) => {
          const index = seat(spot);
          const piece = board.squares[index];
          const usi = toUsi(fromIndex(index));
          const label = `${FILES[index % BOARD_SIZE]}${RANKS[Math.floor(index / BOARD_SIZE)]}`;
          // 물러진 수가 지나간 두 칸. 打은 화살표가 나가지 않으므로(ReviewDetail 의
          // `retracted`) 도착 칸 표식이 「어디에 놓으려 했나」를 짚는 하나뿐이다.
          const mark = played?.from === index ? 'from' : played?.to === index ? 'to' : null;
          const last = lastMove?.to === index ? 'to' : lastMove?.from === index ? 'from' : undefined;

          // 이 칸으로 駒가 들어오는 중인가. 자리는 유령 駒와 같이 칸 수로 준다.
          const moving = motion?.to === index ? motion : null;
          const slide =
            moving && moving.from !== null
              ? ({
                  '--mcol': (seat(moving.from) % BOARD_SIZE) - (spot % BOARD_SIZE),
                  '--mrow': Math.floor(seat(moving.from) / BOARD_SIZE) - Math.floor(spot / BOARD_SIZE),
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

        {/* 판만 낮춘다. 빛과 광선은 이 겹 위에 있어서 낮아진 판 위에서 또렷해진다. 판이
            밝으면 흰 광선이 榧색 나무에 묻힌다. */}
        {dimmed && <span className="board-tint" aria-hidden="true" />}

        {/* 王手가 먼저 켜진다. 王手인 줄 모르면 다음 수가 왜 그것인지도 읽히지 않는다. */}
        {checks.map((c) => (
          <RefutationRay key={`${c.from}-${c.to}`} ray={seatRay(c)} waitForGhost={false} dropFrom={null} />
        ))}

        {ray && <RefutationRay ray={seatRay(ray)} waitForGhost={replay !== null} dropFrom={dropFrom} />}

        {/* 힌트는 마지막에 켠다. 상대 쪽 광선과 겹칠 때 가려지면 안 되는 쪽이 이쪽이다 */}
        {hintRay && <RefutationRay ray={seatRay(hintRay)} waitForGhost={false} dropFrom={dropFrom} />}

        {replay && (
          <ReplayKoma
            replay={
              flipped
                ? { ...replay, from: replay.from === null ? null : seat(replay.from), to: seat(replay.to) }
                : replay
            }
          />
        )}

        {/* 게이지는 판 밖으로 번진다(inset 이 음수다). 판 안의 어느 것과도 자리를 다투지
            않아서 마지막에 그려도 아무것도 가리지 않는다. */}
        {mateHeat > 0 && (
          <span className="mate-flame" style={{ '--heat': mateHeat } as CSSProperties} aria-hidden="true" />
        )}
      </div>

      <div className="board-ranks" aria-hidden="true">
        {(flipped ? RANKS.toReversed() : RANKS).map((r) => (
          <span key={r}>{r}</span>
        ))}
      </div>

      {/* 판이 주는 손잡이들. WebGL 이 잡히지 않으면 그늘 쪽은 내놓지 않는다. 눌러도 아무 일도
          일어나지 않는 버튼은 「고장 났다」로 읽힌다. 회상 중에도 잠그지 않는다(`exposed`). */}
      {(ready || sound || flip) && (
        <div className="board-toggles">
          {sound && (
            <button
              type="button"
              className="board-toggle"
              aria-pressed={sound.on}
              title="駒を置く音を鳴らす"
              onClick={sound.toggle}
            >
              駒音
            </button>
          )}
          {ready && (
            <button
              type="button"
              className="board-toggle"
              aria-pressed={exposed}
              title="相手の駒が利いていて、こちらが受けていないマスに影が落ちる"
              onClick={() => setShowExposure((on) => !on)}
            >
              相手の利き
            </button>
          )}
          {flip && (
            <button
              type="button"
              className="board-toggle"
              aria-pressed={flip.on}
              title="盤の上下を入れかえる"
              onClick={flip.toggle}
            >
              盤を反転
            </button>
          )}
        </div>
      )}
    </div>
  );
}
