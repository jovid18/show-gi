import { useEffect, useRef, useState } from 'react';

import { Koma } from '@/components/Koma';
import { HAND_ORDER, kanjiOf, nameOf, type Piece, type Side } from '@/models/piece';
import type { Board as BoardModel } from '@/models/sfen';

/**
 * 사진에서 읽어 온 판을 사람이 한 칸씩 고치는 자리(journal §129).
 *
 * 대국판(`Board`)을 쓰지 않는다. 저쪽은 빛과 색이 채널로 정해져 있어(docs/01-core.md §7)
 * 빌려 쓰면 한 색이 「이 칸이 규칙을 어겼다」까지 말하게 된다. 격자와 駒台를 따로 그리고 나눠
 * 쓰는 것은 駒 하나뿐이다(`Koma`).
 *
 * 여기서 규칙을 판단하지 않는다. 二歩가 되는 자리에도 그냥 놓이고, 성립하는 판인가는 서버가
 * 답한다(`/api/position/check`). 여기서 막으면 사람이 고쳐 가는 중간 상태를 그릴 수 없다.
 *
 * 화면의 낱말이 「あなた」와 「相手」라 先手·後手가 한 번도 나오지 않는다.
 */
interface PositionEditorProps {
  board: BoardModel;
  /** 규칙을 어긴 칸(화면 배열 인덱스). 붉은 점선으로 두른다. */
  faults: ReadonlySet<number>;
  onChange: (next: BoardModel) => void;
}

const BOARD_SIZE = 9;

/** 골라 놓을 수 있는 駒. 판에는 양쪽 玉이 있어야 해서 玉도 목록에 있다. */
const PLACEABLE = ['P', 'L', 'N', 'S', 'G', 'B', 'R', 'K'] as const;

/** 成 을 붙일 수 있는 종류. 金과 玉에는 뒷면이 없다. */
const PROMOTABLE = new Set(['P', 'L', 'N', 'S', 'B', 'R']);

/** 아래쪽이 자기 편이다. 사진을 찍은 사람의 시점이다. */
const NEAR: Side = 'black';
const FAR: Side = 'white';

export function PositionEditor({ board, faults, onChange }: PositionEditorProps) {
  const [picking, setPicking] = useState<number | null>(null);

  const put = (square: number, piece: Piece | null): void => {
    const squares = [...board.squares];
    squares[square] = piece;
    onChange({ ...board, squares });
    setPicking(null);
  };

  const setHand = (side: Side, kind: string, n: number): void => {
    // 40장이 한 판의 전부라 그 위의 값은 판에 없다.
    const count = Math.max(0, Math.min(40, n));
    onChange({
      ...board,
      hands: { ...board.hands, [side]: { ...board.hands[side], [kind]: count } },
    });
  };

  return (
    <div className="pos-edit">
      <HandTray side={FAR} label="相手の持ち駒" pieces={board.hands[FAR]} onSet={(kind, n) => setHand(FAR, kind, n)} />

      <div className="pos-edit__board" role="grid" aria-label="読み取った局面">
        {board.squares.map((piece, square) => (
          <Square
            key={square}
            square={square}
            piece={piece ?? null}
            faulty={faults.has(square)}
            open={picking === square}
            onOpen={() => setPicking(picking === square ? null : square)}
            onPick={(next) => put(square, next)}
            onClose={() => setPicking(null)}
          />
        ))}
      </div>

      <HandTray
        side={NEAR}
        label="あなたの持ち駒"
        pieces={board.hands[NEAR]}
        onSet={(kind, n) => setHand(NEAR, kind, n)}
      />
    </div>
  );
}

interface SquareProps {
  square: number;
  piece: Piece | null;
  faulty: boolean;
  open: boolean;
  onOpen: () => void;
  onPick: (piece: Piece | null) => void;
  onClose: () => void;
}

/**
 * 칸 하나. 누르면 그 자리에 팝오버가 뜬다.
 *
 * 칸에 좌표를 적지 않는다. 판 옆의 筋·段 눈금이 이미 그 일을 하고, 글자를 넣으면 駒 글자와
 * 겹쳐 읽힌다. 스크린리더에는 「몇筋 몇段」을 준다.
 */
function Square({ square, piece, faulty, open, onOpen, onPick, onClose }: SquareProps) {
  const file = BOARD_SIZE - (square % BOARD_SIZE);
  const rank = Math.floor(square / BOARD_SIZE) + 1;
  const label =
    `${file}筋${rank}段` + (piece ? ` ${piece.side === NEAR ? 'あなた' : '相手'}の${nameOf(piece.kind)}` : ' 空');

  return (
    <div className="pos-square" data-faulty={faulty || undefined} data-open={open || undefined}>
      <button type="button" className="pos-square__hit" aria-label={label} aria-expanded={open} onClick={onOpen}>
        {piece && <Koma kind={piece.kind} side={piece.side} marks={false} />}
      </button>
      {open && <Picker current={piece} onPick={onPick} onClose={onClose} />}
    </div>
  );
}

/**
 * 무엇을 놓을지 고르는 팝오버.
 *
 * 편과 成은 종류를 다시 고르지 않고 토글 하나로 넘어간다. 그 둘이 「종류는 맞는데 뭔가
 * 다르다」의 전부이고, 어느 쪽이 잦은지는 재지 않았다(journal §129).
 */
function Picker({
  current,
  onPick,
  onClose,
}: {
  current: Piece | null;
  onPick: (piece: Piece | null) => void;
  onClose: () => void;
}) {
  const [side, setSide] = useState<Side>(current?.side ?? NEAR);
  const [promoted, setPromoted] = useState(current?.kind.startsWith('+') ?? false);
  const ref = useRef<HTMLDivElement>(null);

  const flip = (next: Side): void => {
    setSide(next);
    if (current) onPick({ ...current, side: next });
  };

  const toggle = (next: boolean): void => {
    setPromoted(next);
    if (!current) return;
    const base = current.kind.replace('+', '');
    if (!PROMOTABLE.has(base)) return;
    onPick({ kind: next ? `+${base}` : base, side: current.side });
  };

  /** 바깥을 누르거나 Escape 면 닫는다. 팝오버가 판을 덮은 채로 남으면 다른 칸을 누를 수 없다. */
  useEffect(() => {
    const onDown = (e: MouseEvent): void => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') onClose();
    };
    // 이 팝오버를 연 클릭이 그대로 바깥 클릭으로 잡히지 않게 다음 틱부터 듣는다.
    const id = window.setTimeout(() => document.addEventListener('mousedown', onDown), 0);
    document.addEventListener('keydown', onKey);
    return () => {
      window.clearTimeout(id);
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [onClose]);

  return (
    <div className="pos-picker" ref={ref} role="dialog" aria-label="この駒を直す">
      <div className="pos-picker__row" role="group" aria-label="どちらの駒か">
        <button type="button" className="pos-picker__toggle" aria-pressed={side === NEAR} onClick={() => flip(NEAR)}>
          あなた
        </button>
        <button type="button" className="pos-picker__toggle" aria-pressed={side === FAR} onClick={() => flip(FAR)}>
          相手
        </button>
        <button type="button" className="pos-picker__toggle" aria-pressed={promoted} onClick={() => toggle(!promoted)}>
          成
        </button>
      </div>

      <div className="pos-picker__pieces">
        {PLACEABLE.map((base) => {
          const kind = promoted && PROMOTABLE.has(base) ? `+${base}` : base;
          return (
            <button
              type="button"
              key={base}
              className="pos-picker__piece"
              aria-label={nameOf(kind)}
              aria-pressed={current?.kind === kind && current.side === side}
              onClick={() => onPick({ kind, side })}
            >
              <Koma kind={kind} side={side} marks={false} />
            </button>
          );
        })}
      </div>

      <button type="button" className="pos-picker__clear" onClick={() => onPick(null)}>
        このマスを空にする
      </button>
    </div>
  );
}

/**
 * 駒台 하나. 종류마다 개수를 세는 자리다.
 *
 * 대국의 `Hand` 를 쓰지 않는다. 저쪽은 누르는 것이 곧 착수인데, 여기서 필요한 것은 숫자를
 * 올리고 내리는 일이다.
 *
 * 없는 종류도 줄을 지킨다. 0을 감추면 「歩가 몇 장이었지」를 확인하러 온 사람이 그 종류의
 * 자리를 찾지 못한다.
 */
function HandTray({
  side,
  label,
  pieces,
  onSet,
}: {
  side: Side;
  label: string;
  pieces: Record<string, number>;
  onSet: (kind: string, n: number) => void;
}) {
  return (
    <div className="pos-hand" data-side={side}>
      <span className="pos-hand__label">{label}</span>
      <div className="pos-hand__rows">
        {HAND_ORDER.map((kind) => {
          const n = pieces[kind] ?? 0;
          return (
            <div className="pos-hand__row" key={kind} data-held={n > 0 || undefined}>
              <span className="pos-hand__kanji" aria-hidden="true">
                {kanjiOf(kind)}
              </span>
              <button
                type="button"
                className="pos-hand__step"
                aria-label={`${nameOf(kind)}を一枚減らす`}
                disabled={n === 0}
                onClick={() => onSet(kind, n - 1)}
              >
                −
              </button>
              <span className="pos-hand__count" aria-label={`${nameOf(kind)} ${n}枚`}>
                {n}
              </span>
              <button
                type="button"
                className="pos-hand__step"
                aria-label={`${nameOf(kind)}を一枚増やす`}
                onClick={() => onSet(kind, n + 1)}
              >
                ＋
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
