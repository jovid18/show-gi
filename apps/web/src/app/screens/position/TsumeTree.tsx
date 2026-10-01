import { useMemo, useState } from 'react';

import { Board } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { lastMoveOf } from '@/libs/game/board-view';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import type { TsumeMove, TsumeResponse } from '@/protocol/tsume';

/**
 * 詰め将棋의 답. 왼쪽에 판, 오른쪽에 수순이다. 수를 누르면 그 수를 둔 뒤의 판을 그린다.
 *
 * 수순은 本手順 한 줄로 펼친다. 서버가 응수를 긴 것부터 주므로 첫 응수를 따라가면 수비 쪽이
 * 가장 오래 버티는 줄이 된다. 다른 응수는 그 응수 아래 「変化」로 접어 두고, 열면 그 응수에서
 * 시작하는 줄이 같은 모양으로 펼쳐진다. 無駄合い는 펼치지 않고 이름만 적는다.
 */
export function TsumeTree({ start, res }: { start: string; res: TsumeResponse }) {
  const [picked, setPicked] = useState<TsumeMove | null>(null);
  const sfen = picked?.sfen ?? start;
  const board = useMemo<BoardModel | null>(() => {
    try {
      return parseSfen(sfen);
    } catch {
      return null;
    }
  }, [sfen]);

  return (
    <div className="tsume">
      <p className="tsume__verdict" data-status={res.status}>
        {res.message}
      </p>
      {res.tree && (
        <div className="tsume__body">
          <div className="tsume__board quiz-board">
            {board && (
              <>
                <Hand
                  side="white"
                  label="相手"
                  pieces={board.hands.white}
                  selected={null}
                  playable={new Set()}
                  onPick={() => {}}
                />
                <Board
                  board={board}
                  lit={new Set()}
                  selected={null}
                  lastMove={picked ? lastMoveOf(picked.usi) : null}
                  checked={null}
                  played={null}
                  replay={null}
                  rays={[]}
                  motion={null}
                  checks={[]}
                  dimmed={false}
                  dropFrom={{}}
                  hintSquare={null}
                  hintRay={null}
                  mateHeat={0}
                  me="black"
                  flipped={false}
                  interactive={false}
                  onSquare={() => {}}
                />
                <Hand
                  side="black"
                  label="あなた"
                  pieces={board.hands.black}
                  selected={null}
                  playable={new Set()}
                  onPick={() => {}}
                />
              </>
            )}
            <button type="button" className="btn" disabled={picked === null} onClick={() => setPicked(null)}>
              最初の局面
            </button>
          </div>
          <div className="tsume__moves">
            <Line from={res.tree} ply={1} picked={picked} onPick={setPicked} />
          </div>
        </div>
      )}
    </div>
  );
}

interface LineProps {
  from: TsumeMove;
  /** `from` 이 몇 手目인가. */
  ply: number;
  picked: TsumeMove | null;
  onPick: (m: TsumeMove) => void;
}

/**
 * `from` 에서 시작해 첫 응답만 따라가는 한 줄. 각 칸의 `alts` 는 그 수 대신 둘 수 있었던 응수다.
 * 응수가 無駄合い뿐이면 관례로 거기서 詰み라 줄을 끝내고, 無駄合い는 공격 쪽 수 아래에 적는다.
 */
function Line({ from, ply, picked, onPick }: LineProps) {
  const steps: { move: TsumeMove; ply: number; alts: TsumeMove[] }[] = [];
  let move: TsumeMove | undefined = from;
  let alts: TsumeMove[] = [];
  for (let n = ply; move; n++) {
    const step = { move, ply: n, alts };
    steps.push(step);
    const replies: TsumeMove[] = move.replies ?? [];
    const next: TsumeMove | undefined = replies[0];
    if (next?.futile) {
      step.alts = replies;
      break;
    }
    // 수비 쪽 수의 응답은 언제나 하나라 갈래는 공격 쪽 수 뒤에만 생긴다.
    alts = replies.slice(1);
    move = next;
  }

  return (
    <ol className="tsume__line">
      {steps.map((s) => (
        <Row key={s.ply} step={s} picked={picked} onPick={onPick} />
      ))}
    </ol>
  );
}

/**
 * 棋譜 한 줄. 手数·수·갈래 손잡이가 한 행에 선다. 갈래를 열면 그 행 바로 아래에 들여 쓴 줄로
 * 펼쳐져, 어느 수의 変化인지가 자리로 보인다.
 */
function Row({
  step,
  picked,
  onPick,
}: { step: { move: TsumeMove; ply: number; alts: TsumeMove[] } } & Omit<LineProps, 'from' | 'ply'>) {
  const [open, setOpen] = useState(false);
  const real = step.alts.filter((m) => !m.futile);
  const futile = step.alts.filter((m) => m.futile);
  return (
    <li className="tsume__row">
      <div className="tsume__head">
        <span className="tsume__ply">{step.ply}</span>
        <button
          type="button"
          className="tsume__move"
          data-picked={picked === step.move || undefined}
          onClick={() => onPick(step.move)}
        >
          {step.move.ja}
        </button>
        {real.length > 0 && (
          <button type="button" className="tsume__toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? '変化を閉じる' : `ほかの応手 ${real.length}`}
          </button>
        )}
      </div>
      {futile.length > 0 && (
        <p className="tsume__futile">
          <span>無駄合い</span>
          {futile.map((m) => (
            <button
              key={m.usi}
              type="button"
              className="tsume__chip"
              data-picked={picked === m || undefined}
              onClick={() => onPick(m)}
            >
              {m.ja}
            </button>
          ))}
        </p>
      )}
      {open && (
        <ul className="tsume__branches">
          {real.map((m) => (
            <li key={m.usi}>
              <span className="tsume__rest">
                {m.ja}なら、あと{m.rest}手で詰み
              </span>
              <Line from={m} ply={step.ply} picked={picked} onPick={onPick} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}
