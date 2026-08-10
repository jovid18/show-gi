import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';

import { Board, type DropFrom, type LastMove, type Ray, type Replay } from './Board';
import { Hand } from './Hand';
import { Intervention } from './Intervention';
import { Kifu } from './Kifu';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/game/moves';
import type { Player, Snapshot } from '@/game/protocol';
import { useGame } from '@/game/useGame';
import type { Side } from '@/shogi/piece';
import { parseSfen, type Board as BoardModel } from '@/shogi/sfen';
import { fromIndex, fromUsi, toIndex, toUsi } from '@/shogi/square';

/**
 * 직전 수가 지나간 두 칸.
 *
 * **도착만 짚으면 「저기 뭔가 있다」까지다.** 초심자가 알아야 하는 것은 무엇이 어디서
 * 왔나이고, 출발 칸이 비었다는 사실이 그 절반이다.
 */
function lastMoveOf(usi: string): LastMove | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    const to = toIndex(fromUsi(move.to));
    return { from: move.kind === 'drop' ? null : toIndex(fromUsi(move.from)), to };
  } catch {
    return null; // 못 읽는 좌표로 엉뚱한 칸을 칠하느니 안 칠한다
  }
}

/**
 * 회상 한 장면.
 *
 * **화살표는 방금 둔 수가 아니라 다음에 올 수다.** 판은 이미 벌어진 것을 보여주고 있으니
 * 거기에 지나간 궤적을 겹치면 같은 말을 두 번 하는 것이고, 알고 싶은 것은 「그래서 상대가
 * 어떻게 하는가」다. `次へ` 를 누르면 화살표대로 벌어진다.
 *
 * 그래서 채널이 갈린다 — **흰빛 두 칸은 방금 벌어진 것**, **초록 화살표는 다음에 벌어질 것.**
 */
interface Scene {
  board: BoardModel;
  /** 이 판을 만든 수. 흰빛 두 칸으로 짚는다. */
  played: LastMove;
  /** 다음에 올 수. 마지막 장면에는 없다. */
  ray: Ray | null;
  /** 화살표가 가리키는 수가 수순의 몇 번째인가. 마지막 장면이면 -1. */
  next: number;
  /** 다음 수가 打이면 그 駒. 駒台에서 같이 빛나 화살표의 짝이 된다. */
  dropping: { side: Side; kind: string } | null;
}

function rayOf(usi: string, by: Player): Ray | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    return { from: move.kind === 'drop' ? null : toIndex(fromUsi(move.from)), to: toIndex(fromUsi(move.to)), by };
  } catch {
    return null; // 못 읽는 좌표로 엉뚱한 화살표를 긋느니 안 긋는다
  }
}

/**
 * `ancestor` 안에서의 자리(px). `offsetParent` 를 타고 올라가며 더한다.
 *
 * **`getBoundingClientRect` 를 안 쓴다.** 개입 중에는 판이 3D로 기울어 있어서 그쪽은
 * 변형이 끝난 화면 좌표를 주는데, 우리가 필요한 것은 변형 **전**의 배치 좌표다.
 * `offsetLeft/offsetTop` 이 그 값이고, 화살표도 같은 변형 안에 있으므로 그대로 맞는다.
 */
function offsetWithin(el: HTMLElement, ancestor: HTMLElement): { x: number; y: number } | null {
  let x = 0;
  let y = 0;
  let node: HTMLElement | null = el;
  while (node && node !== ancestor) {
    x += node.offsetLeft;
    y += node.offsetTop;
    node = node.offsetParent as HTMLElement | null;
  }
  return node === ancestor ? { x, y } : null;
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
  // 회상의 몇 번째 장면을 보고 있는가. 0이 물러진 수 자체다.
  const [scene, setScene] = useState(0);
  // 打 화살표가 출발할 자리. 駒台는 판 밖이라 칸 산수로는 안 나오고 재야 한다.
  const [dropFrom, setDropFrom] = useState<DropFrom | null>(null);
  const boardRef = useRef<HTMLDivElement>(null);
  const dropPieceRef = useRef<HTMLButtonElement | null>(null);

  // 새 대국은 판만이 아니라 고르던 것까지 전부 비우고 시작한다.
  const newGame = (): void => {
    setOrigin(null);
    setPending(null);
    setConfirmingResign(false);
    restart();
  };

  // 지금 대국의 판. 회상 중에는 화면에 안 나오지만 착수 후보와 유령 駒는 여기서 나온다.
  const live = useMemo(() => {
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
  const lastMove = useMemo(() => (last ? lastMoveOf(last.usi) : null), [last]);

  // 王手를 받고 있는 玉의 칸. 강조는 판 위에서만 하고 글로는 반복하지 않는다.
  const checked = useMemo(() => {
    if (!live || !snapshot?.inCheck) return null;
    const index = live.squares.findIndex((p) => p?.kind === 'K' && p.side === live.turn);
    return index < 0 ? null : toUsi(fromIndex(index));
  }, [live, snapshot?.inCheck]);

  const intervention = snapshot?.intervention ?? null;
  const intervening = intervention !== null && interventionEpisode > seenEpisode;

  /**
   * 회상의 각 장면.
   *
   * 0번이 **물러진 수를 둔 직후**이고 그 뒤가 상대의 반박 수순이다. 판은 장면마다 서버가
   * 준 국면을 그대로 그린다 — **화면이 수를 두지 않는다.** 두게 하면 규칙 엔진을 한 벌
   * 더 갖는 것이고, 그건 D2에서 「클라이언트는 규칙을 모른다」로 정해둔 자리다.
   *
   * 넘겨 보게 만드는 것이 곧 정직해지는 길이기도 하다. 한 판 위에 수순을 겹쳐 그리면
   * 「상대가 아직 손에 없는 駒를 놓는 수」까지 그리게 된다.
   */
  const scenes = useMemo<Scene[]>(() => {
    const line = intervention?.refutation;
    if (!intervention || !line?.length) return [];

    // i번째 장면 = i수까지 진행한 판. 0번은 물러진 수를 둔 직후다.
    const sfenAt = (i: number): string => (i === 0 ? intervention.retractedSfen : (line[i - 1]?.sfen ?? ''));
    const playedAt = (i: number): string => (i === 0 ? intervention.retractedUsi : (line[i - 1]?.usi ?? ''));

    const out: Scene[] = [];
    for (let i = 0; i <= line.length; i++) {
      const played = lastMoveOf(playedAt(i));
      if (!played) break; // 어긋난 장면으로 넘기느니 거기서 멈춘다
      let board: BoardModel;
      try {
        board = parseSfen(sfenAt(i));
      } catch {
        break;
      }
      const upcoming = line[i];
      const parsed = upcoming ? parseUsi(upcoming.usi) : null;
      out.push({
        board,
        played,
        ray: upcoming ? rayOf(upcoming.usi, upcoming.by) : null,
        next: upcoming ? i : -1,
        // 화면은 늘 相手가 白·あなた가 黑이다.
        dropping:
          parsed?.kind === 'drop' && upcoming
            ? { side: upcoming.by === 'engine' ? 'white' : 'black', kind: parsed.piece }
            : null,
      });
    }
    return out;
  }, [intervention]);

  // 같은 수로 또 걸렸을 때도 처음부터 본다. 회차가 오르면 장면도 돌아간다.
  useEffect(() => setScene(0), [interventionEpisode]);

  const walking = intervening && scenes.length > 0;
  const current = walking ? scenes[Math.min(scene, scenes.length - 1)] : null;
  const dropping = current?.dropping ?? null;

  // 화면 폭이 바뀌면 `--sq` 가 따라 변하므로 그때마다 다시 잰다.
  const measureDrop = useCallback(() => {
    const board = boardRef.current;
    const piece = dropPieceRef.current;
    const stage = board?.closest('.game-board');
    if (!board || !piece || !(stage instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    const at = offsetWithin(piece, stage);
    const of = offsetWithin(board, stage);
    const square = board.firstElementChild;
    if (!at || !of || !(square instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    setDropFrom({
      // 판의 테두리 안쪽이 기준이다 — 화살표가 그 안에 놓이므로.
      x: at.x + piece.offsetWidth / 2 - (of.x + board.clientLeft),
      y: at.y + piece.offsetHeight / 2 - (of.y + board.clientTop),
      sq: square.offsetWidth,
    });
  }, []);

  useLayoutEffect(() => {
    if (!dropping) {
      setDropFrom(null);
      return;
    }
    measureDrop();
    const stage = boardRef.current?.closest('.game-board');
    if (!(stage instanceof HTMLElement)) return;
    const observer = new ResizeObserver(measureDrop);
    observer.observe(stage);
    return () => observer.disconnect();
  }, [dropping, measureDrop]);

  /**
   * 물러진 수가 지나간 두 칸.
   *
   * 판은 이미 그 수를 둔 국면이므로 **유령 駒는 첫 장면에서 한 번만** 난다. 어느 駒였는지는
   * 되돌아온 판(`snapshot.sfen`)의 출발 칸에서 읽는다 — 성한 수라면 도착 칸에는 이미
   * 성한 駒가 서 있어서, 날아가는 것이 무엇이었는지가 거기엔 없다.
   */
  const replay = useMemo<Replay | null>(() => {
    if (!intervening || !intervention || !live) return null;
    if (walking && scene !== 0) return null;

    const move = parseUsi(intervention.retractedUsi);
    if (!move) return null;

    try {
      const to = toIndex(fromUsi(move.to));
      if (move.kind === 'drop') {
        // 打은 출발 칸이 없다. 잡는 쪽은 지금 차례인 사람이다.
        return { from: null, to, kind: move.piece, side: live.turn };
      }
      const from = toIndex(fromUsi(move.from));
      const piece = live.squares[from];
      // 비어 있으면 판과 개입이 어긋난 것이다. 틀린 것을 그리느니 안 그린다.
      if (!piece) return null;
      return { from, to, kind: piece.kind, side: piece.side };
    } catch {
      return null;
    }
  }, [intervening, intervention, live, walking, scene]);

  // 화면에 그리는 판. 회상 중에는 그 장면의 국면이고, 아니면 지금 대국의 판이다.
  const board = current?.board ?? live;

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
          dropping={dropping?.side === 'white' ? dropping.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          onPick={() => {}}
        />

        <Board
          board={board}
          lit={lit}
          selected={origin && !origin.endsWith('*') ? origin : null}
          lastMove={walking ? null : lastMove}
          checked={walking ? null : checked}
          played={current?.played ?? null}
          replay={replay}
          ray={current?.ray ?? null}
          dimmed={walking}
          dropFrom={dropFrom}
          boardRef={boardRef}
          interactive={playable}
          onSquare={onSquare}
        />

        <Hand
          side="black"
          label="あなた"
          pieces={board.hands.black}
          selected={origin?.endsWith('*') ? origin : null}
          playable={new Set([...grouped.keys()].filter((o) => o.endsWith('*')))}
          dropping={dropping?.side === 'black' ? dropping.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
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
              scene={walking ? Math.min(scene, scenes.length - 1) : null}
              scenes={scenes.length}
              highlight={current ? (current.next >= 0 ? current.next : scenes.length - 2) : -1}
              onStep={setScene}
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
