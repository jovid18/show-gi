import { useMemo, useState } from 'react';

import { Board } from './Board';
import { Hand } from './Hand';
import { Kifu } from './Kifu';
import { groupByOrigin, toUsiMove, type Destination } from '@/game/moves';
import type { Snapshot } from '@/game/protocol';
import { useGame } from '@/game/useGame';
import { parseSfen } from '@/shogi/sfen';
import { fromIndex, toUsi } from '@/shogi/square';

/** 직전 수가 도착한 칸. `P*5e` → `5e`, `8h2b+` → `2b` — 어느 쪽이든 3·4번째 글자다. */
function destinationOf(usi: string): string {
  return usi.slice(2, 4);
}

function resultText(snapshot: Snapshot): string | null {
  const won = snapshot.winner === 'human';
  switch (snapshot.status) {
    case 'checkmate':
      return won ? '詰み。あなたの勝ちです。' : '詰み。あなたの負けです。';
    case 'stalemate':
      return won ? '手詰まり。あなたの勝ちです。' : '手詰まり。あなたの負けです。';
    case 'resigned':
      return won ? '相手が投了しました。あなたの勝ちです。' : '投了しました。';
    case 'repetition':
      return '千日手。引き分けです。';
    default:
      return null;
  }
}

export function GameScreen() {
  const { connection, snapshot, rejection, play, resign, dismissRejection, restart } = useGame();
  const [origin, setOrigin] = useState<string | null>(null);
  const [pending, setPending] = useState<{ origin: string; to: string } | null>(null);
  const [confirmingResign, setConfirmingResign] = useState(false);

  // 새 대국은 판만이 아니라 고르던 것까지 전부 비우고 시작한다.
  const newGame = (): void => {
    setOrigin(null);
    setPending(null);
    setConfirmingResign(false);
    restart();
  };

  const board = useMemo(() => {
    if (!snapshot) return null;
    try {
      return parseSfen(snapshot.sfen);
    } catch {
      return null; // 판을 못 읽으면 그리지 않는다. 틀린 판을 그리는 것보다 낫다
    }
  }, [snapshot]);

  const grouped = useMemo(() => groupByOrigin(snapshot?.legalMoves ?? []), [snapshot?.legalMoves]);

  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);

  const moves = snapshot?.moves ?? [];
  const last = moves.at(-1);
  const lastMove = last ? destinationOf(last.usi) : null;

  // 王手를 받고 있는 玉의 칸. 강조는 판 위에서만 하고 글로는 반복하지 않는다.
  const checked = useMemo(() => {
    if (!board || !snapshot?.inCheck) return null;
    const index = board.squares.findIndex((p) => p?.kind === 'K' && p.side === board.turn);
    return index < 0 ? null : toUsi(fromIndex(index));
  }, [board, snapshot?.inCheck]);

  if (connection === 'closed') {
    return (
      <div className="notice" role="status">
        <p>接続が切れました。</p>
        <button type="button" className="btn" onClick={newGame}>
          もう一度つなぐ
        </button>
      </div>
    );
  }
  if (!snapshot || !board) {
    return (
      <p className="notice" role="status">
        接続しています。
      </p>
    );
  }

  const playable = snapshot.yourTurn && snapshot.status === 'playing' && !pending;
  const result = resultText(snapshot);

  const pick = (next: string): void => {
    dismissRejection();
    setOrigin(next === origin ? null : next);
  };

  const commit = (to: string): void => {
    if (!origin) return;
    const dest = destinations.find((d) => d.to === to);
    if (!dest) return;

    // 성/불성이 둘 다 가능할 때만 묻는다. 강제 승격은 목록에 성만 들어 있으므로 안 묻는다.
    if (dest.plain && dest.promote) {
      setPending({ origin, to });
      return;
    }
    play(toUsiMove(origin, to, dest.promote));
    setOrigin(null);
  };

  const onSquare = (usi: string): void => {
    if (!playable) return;
    if (origin && lit.has(usi)) {
      commit(usi);
      return;
    }
    if (grouped.has(usi)) {
      pick(usi);
      return;
    }
    setOrigin(null);
  };

  const finishPromotion = (promote: boolean): void => {
    if (!pending) return;
    play(toUsiMove(pending.origin, pending.to, promote));
    setPending(null);
    setOrigin(null);
  };

  return (
    <div className="game">
      <div className="game-board">
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
          lit={lit}
          selected={origin && !origin.endsWith('*') ? origin : null}
          lastMove={lastMove}
          checked={checked}
          interactive={playable}
          onSquare={onSquare}
        />

        <Hand
          side="black"
          label="あなた"
          pieces={board.hands.black}
          selected={origin?.endsWith('*') ? origin : null}
          playable={new Set([...grouped.keys()].filter((o) => o.endsWith('*')))}
          onPick={playable ? pick : () => {}}
        />
      </div>

      <aside className="game-side">
        <p className="status" data-tone={result ? 'result' : snapshot.thinking ? 'wait' : 'turn'}>
          {result ?? (snapshot.thinking ? '相手が考えています。' : 'あなたの番です。')}
        </p>

        {rejection && (
          <p className="rejection" role="alert">
            {rejection}
          </p>
        )}

        {pending && (
          <div className="promotion" role="group" aria-label="成りの選択">
            <span>成りますか。</span>
            <button type="button" className="btn btn--primary" onClick={() => finishPromotion(true)}>
              成る
            </button>
            <button type="button" className="btn" onClick={() => finishPromotion(false)}>
              不成
            </button>
          </div>
        )}

        <Kifu moves={moves} />

        {snapshot.status !== 'playing' && (
          <button type="button" className="btn btn--primary" onClick={newGame}>
            もう一局
          </button>
        )}

        {snapshot.status === 'playing' &&
          (confirmingResign ? (
            <div className="resign-confirm" role="group" aria-label="投了の確認">
              <span>投了しますか。</span>
              <button
                type="button"
                className="btn btn--danger"
                onClick={() => {
                  resign();
                  setConfirmingResign(false);
                }}
              >
                投了する
              </button>
              <button type="button" className="btn" onClick={() => setConfirmingResign(false)}>
                やめる
              </button>
            </div>
          ) : (
            <button type="button" className="btn" onClick={() => setConfirmingResign(true)}>
              投了
            </button>
          ))}
      </aside>
    </div>
  );
}
