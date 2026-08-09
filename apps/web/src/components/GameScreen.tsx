import { useMemo, useState } from 'react';

import { Board, type Replay } from './Board';
import { Hand } from './Hand';
import { Intervention } from './Intervention';
import { Kifu } from './Kifu';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/game/moves';
import type { Snapshot } from '@/game/protocol';
import { useGame } from '@/game/useGame';
import { parseSfen } from '@/shogi/sfen';
import { fromIndex, fromUsi, toIndex, toUsi } from '@/shogi/square';

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
  const { connection, snapshot, rejection, interventionEpisode, play, resign, dismissRejection, restart } = useGame();
  const [origin, setOrigin] = useState<string | null>(null);
  const [pending, setPending] = useState<{ origin: string; to: string } | null>(null);
  const [confirmingResign, setConfirmingResign] = useState(false);
  // 어느 회차까지 봤는가. 서버는 다음 착수까지 개입을 들고 있으므로 「있다」로 판정하면
  // 닫아도 다시 뜬다. 회차를 비교하면 같은 수로 또 걸렸을 때만 다시 연다.
  const [seenEpisode, setSeenEpisode] = useState(0);

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

  const intervention = snapshot?.intervention ?? null;
  const intervening = intervention !== null && interventionEpisode > seenEpisode;

  /**
   * 물러진 수가 지나간 두 칸.
   *
   * 판은 이미 되돌아와 있으므로 **던질 뻔한 駒는 출발 칸에 그대로 서 있다.** 그걸 읽어서
   * 유령 駒를 만든다 — 서버가 준 USI 말고는 아무것도 추론하지 않는다.
   */
  const replay = useMemo<Replay | null>(() => {
    if (!intervening || !intervention || !board) return null;
    const move = parseUsi(intervention.retractedUsi);
    if (!move) return null;

    try {
      const to = toIndex(fromUsi(move.to));
      if (move.kind === 'drop') {
        // 打은 출발 칸이 없다. 잡는 쪽은 지금 차례인 사람이다.
        return { from: null, to, kind: move.piece, side: board.turn };
      }
      const from = toIndex(fromUsi(move.from));
      const piece = board.squares[from];
      // 비어 있으면 판과 개입이 어긋난 것이다. 틀린 것을 그리느니 안 그린다.
      if (!piece) return null;
      return { from, to, kind: piece.kind, side: piece.side };
    } catch {
      return null;
    }
  }, [intervening, intervention, board]);

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

  // 제지형 개입은 **시간을 멈춘다**(docs/03-frontend.md §2). 닫을 때까지 둘 수 없다.
  // 판정 중(`judging`)에는 서버가 이미 `yourTurn`을 내려두므로 여기서 더 할 것이 없다.
  const playable = snapshot.yourTurn && snapshot.status === 'playing' && !pending && !intervening;
  const result = resultText(snapshot);

  // 개입 중에도 여기는 차례만 말한다. 물러진 뒤에는 실제로 다시 사람 차례이고,
  // 무엇을 물렀는지는 바로 위 문구가 이미 말한다 — 같은 말을 두 번 하지 않는다.
  const statusText =
    result ??
    (snapshot.judging ? '今の手を確かめています。' : snapshot.thinking ? '相手が考えています。' : 'あなたの番です。');
  const statusTone = result ? 'result' : snapshot.judging || snapshot.thinking ? 'wait' : 'turn';

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
    <div className="game" data-intervening={intervening || undefined}>
      {/* 판만 남기고 어두워진다. 클릭은 막지 않는다 — 잠글 것은 이미 판 쪽에서 잠겼고,
          투료까지 못 하게 만들 이유가 없다. */}
      {intervening && <div className="veil" aria-hidden="true" />}

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
          replay={replay}
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
        {intervening &&
          intervention && (
            // 회차를 key로 준다. 같은 수로 또 걸렸을 때 컴포넌트가 새로 만들어져야
            // 등장 연출과 초점 이동이 다시 돈다.
            <Intervention
              key={interventionEpisode}
              intervention={intervention}
              onDismiss={() => setSeenEpisode(interventionEpisode)}
            />
          )}

        <p className="status" data-tone={statusTone}>
          {statusText}
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
