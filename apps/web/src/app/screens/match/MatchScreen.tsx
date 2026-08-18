import { useEffect, useMemo, useState } from 'react';

import { Board } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { groupByOrigin, toUsiMove, type Destination } from '@/libs/game/moves';
import { lastMoveOf } from '@/libs/game/board-view';
import { Kifu } from '@/screens/game/Kifu';
import { useMatch, useTurnClock, type MatchConnection } from '@/hooks/useMatch';
import { fetchRoom, type MatchSnapshot, type Room } from '@/protocol/match';
import type { Side } from '@/models/piece';
import { parseSfen } from '@/models/sfen';
import { fromIndex, toUsi } from '@/models/square';
import { hrefOf, navigate } from '@/routes/router';
import { Join } from './Join';
import { Unavailable } from './Unavailable';
import { Waiting } from './Waiting';

/**
 * 사람과 두는 판.
 *
 * **엔진 대국 화면과 갈라 둔 이유는 여기 없는 것들 때문이다** — 개입 카드도, 힌트도,
 * 待った도, 詰み 게이지도, 상대의 강함 눈금도 없다. 그것들을 조건으로 감싸 한 화면에
 * 넣으면 파일이 두 제품을 그리게 되고, 개입 쪽을 고칠 때마다 이쪽이 안 깨졌는지를
 * 매번 확인해야 한다(docs/journal §83).
 *
 * 대신 여기에만 있는 것이 둘이다: **시계**와 **상대의 접속**.
 */
const EMPTY_SET: ReadonlySet<string> = new Set();
const noop = (): void => {};

export function MatchScreen({ roomId }: { roomId: string }) {
  /**
   * **붙기 전에 한 번 물어본다.**
   *
   * WebSocket 이 붙는 순간 손님 자리가 확정되고(서버의 `Hub.Enter`) 시계가 돌기 시작한다 —
   * 그러면 링크를 잘못 누른 사람이 모르는 사이에 남의 방 자리를 태우고, 그 방은 그때부터
   * 아무도 못 들어간다(정원 2명). 방을 만든 사람과 이미 앉은 사람은 그냥 지나간다.
   */
  const [peek, setPeek] = useState<{ room: Room | null; done: boolean }>({ room: null, done: false });
  const [joined, setJoined] = useState(false);

  useEffect(() => {
    const ac = new AbortController();
    void fetchRoom(roomId, ac.signal)
      .then((room) => setPeek({ room, done: true }))
      // 못 읽어도 화면은 선다 — 아래가 「열 수 없다」를 그린다. 실패와 404를 같이 두는
      // 것은 서버가 이미 그 둘을 같은 답으로 주기 때문이다.
      .catch(() => setPeek({ room: null, done: true }));
    return () => ac.abort();
  }, [roomId]);

  if (!peek.done) return <p className="review-status">対局部屋を確認しています…</p>;
  if (!peek.room) return <Unavailable />;
  if (!peek.room.isHost && peek.room.waiting && !joined) {
    return <Join room={peek.room} onJoin={() => setJoined(true)} />;
  }
  return <MatchLive roomId={roomId} />;
}

function MatchLive({ roomId }: { roomId: string }) {
  const { connection, room, snapshot, rejection, gameId, play, resign, dismissRejection } = useMatch(roomId);
  const [origin, setOrigin] = useState<string | null>(null);
  const [pending, setPending] = useState<{ origin: string; to: string } | null>(null);
  const [confirmingResign, setConfirmingResign] = useState(false);

  useEffect(() => {
    document.title = '対人戦 | show-gi';
  }, []);

  if (!snapshot) {
    return <Waiting connection={connection} room={room} roomId={roomId} rejection={rejection} />;
  }
  return (
    <MatchBoard
      connection={connection}
      snapshot={snapshot}
      rejection={rejection}
      gameId={gameId}
      origin={origin}
      pending={pending}
      confirmingResign={confirmingResign}
      setOrigin={setOrigin}
      setPending={setPending}
      setConfirmingResign={setConfirmingResign}
      play={play}
      resign={resign}
      dismissRejection={dismissRejection}
    />
  );
}

interface BoardProps {
  connection: MatchConnection;
  snapshot: MatchSnapshot;
  rejection: string | null;
  gameId: number | null;
  origin: string | null;
  pending: { origin: string; to: string } | null;
  confirmingResign: boolean;
  setOrigin: (v: string | null) => void;
  setPending: (v: { origin: string; to: string } | null) => void;
  setConfirmingResign: (v: boolean) => void;
  play: (usi: string) => void;
  resign: () => void;
  dismissRejection: () => void;
}

function MatchBoard({
  connection,
  snapshot,
  rejection,
  gameId,
  origin,
  pending,
  confirmingResign,
  setOrigin,
  setPending,
  setConfirmingResign,
  play,
  resign,
  dismissRejection,
}: BoardProps) {
  const left = useTurnClock(snapshot);

  const board = useMemo(() => {
    try {
      return parseSfen(snapshot.sfen);
    } catch {
      return null; // 판을 못 읽으면 안 그린다. 틀린 판을 그리는 것보다 낫다
    }
  }, [snapshot.sfen]);

  /** 이 사람이 잡은 쪽. **`turn` 으로 되짚으면 상대 차례에 판이 뒤집힌다.** */
  const me: Side = snapshot.yourColor === 'w' ? 'white' : 'black';
  const them: Side = me === 'black' ? 'white' : 'black';
  const flipped = me === 'white';

  const moves = snapshot.moves ?? [];
  const last = moves.at(-1);
  const lastMove = useMemo(() => (last ? lastMoveOf(last.usi) : null), [last]);

  const checked = useMemo(() => {
    if (!board || !snapshot.inCheck) return null;
    const index = board.squares.findIndex((p) => p?.kind === 'K' && p.side === board.turn);
    return index < 0 ? null : toUsi(fromIndex(index));
  }, [board, snapshot.inCheck]);

  const grouped = useMemo(() => groupByOrigin(snapshot.legalMoves ?? []), [snapshot.legalMoves]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const dropOrigins = useMemo(() => new Set([...grouped.keys()].filter((o) => o.endsWith('*'))), [grouped]);

  // **성/불성을 묻는 동안은 못 둔다.** 그 사이에 다른 수가 나가면 사람이 고른 것과
  // 다른 수가 두어진다(GameScreen 의 `playable` 과 같은 규약).
  const playable = snapshot.yourTurn && snapshot.status === 'playing' && !pending;

  const pick = (next: string): void => {
    dismissRejection();
    setOrigin(next === origin ? null : next);
  };

  const commit = (to: string): void => {
    if (!origin) return;
    const dest = destinations.find((d) => d.to === to);
    if (!dest) return;
    // 성/불성이 둘 다 가능할 때만 묻는다. 강제 승격은 목록에 성만 들어 있다.
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

  if (!board) return <p className="review-status">局面を読み込めませんでした。</p>;

  const result = matchResultText(snapshot);
  const over = snapshot.status !== 'playing';

  return (
    <div className="game">
      <div className="game-main">
        {/* **駒台 라벨은 이름이 아니라 「相手」다.** 표시 이름은 사람이 정하는 값이라
            길 수 있고(`users.display_name`), 이 라벨은 폭이 3.2em 으로 고정이라 넘치면
            줄이 접혀 駒台가 통째로 부푼다 — 실제로 일곱 글자 이름에서 세 줄이 됐다.
            **누구인가는 옆 패널이 말한다**(아래 `.match-who`). */}
        <Hand side={them} label="相手" pieces={board.hands[them]} selected={null} playable={EMPTY_SET} onPick={noop} />

        <Board
          board={board}
          lit={lit}
          selected={origin && !origin.endsWith('*') ? origin : null}
          lastMove={lastMove}
          checked={checked}
          // 아래 일곱은 **개입이 쓰던 자리다.** 대인전에는 개입이 없으므로 늘 비어 있고,
          // 비어 있다는 사실이 이 화면의 내용이다(파일 첫 주석).
          replay={null}
          played={null}
          ray={null}
          motion={null}
          checks={[]}
          dimmed={false}
          dropFrom={null}
          hintSquare={null}
          hintRay={null}
          mateHeat={0}
          me={me}
          flipped={flipped}
          interactive={playable}
          onSquare={onSquare}
        />

        <Hand
          side={me}
          label="あなた"
          pieces={board.hands[me]}
          selected={origin?.endsWith('*') ? origin : null}
          playable={playable ? dropOrigins : EMPTY_SET}
          onPick={playable ? pick : noop}
        />
      </div>

      <aside className="game-side">
        <div className="game-state">
          {/* **상대가 누구인지는 판 내내 서 있어야 한다.** 차례 문구에만 실으면 자기 차례일
              때 그 사람이 화면에서 사라진다 — 대인전에서 「지금 누구와 두고 있나」는 판이
              끝날 때까지 유효한 사실이다. */}
          <p className="match-who">
            対戦相手 <strong>{snapshot.opponentName}</strong>
          </p>

          <p className="status" data-tone={over ? 'result' : snapshot.yourTurn ? 'turn' : 'wait'}>
            {result ?? (snapshot.yourTurn ? 'あなたの番です。' : '相手の番です。')}
          </p>

          {/* **시계는 판이 도는 동안만 그린다.** 끝난 판에 0초가 서 있으면 시간패로
              끝난 것처럼 읽힌다 — 결과 문구가 그것을 이미 말한다. */}
          {!over && <Clock leftMs={left} limitMs={snapshot.turnLimitMs} yours={snapshot.yourTurn} />}

          {/* 상대가 나가 있는 것은 **알리되 판은 안 멈춘다.** 멈추면 지고 있는 쪽이 탭을
              닫아 판을 얼릴 수 있고, 시계가 있는 이유가 정확히 그것이다. */}
          {!over && !snapshot.opponentOnline && (
            <p className="match-away">相手は今、画面を離れています。持ち時間はそのまま進みます。</p>
          )}

          {/* **내 연결이 끊긴 것은 조용히 넘길 수 없다.**
              판은 서버에서 그대로 돌고 **내 시계도 그대로 흐른다** — 화면은 마지막 스냅샷을
              들고 있어서 아무 일도 없어 보이는데, 그동안 시간패로 지고 있다. 그래서 여기만
              색을 쓰고 되돌아가는 길을 같이 준다. */}
          {!over && connection === 'closed' && (
            <div className="match-lost" role="alert">
              <p>接続が切れました。持ち時間は進んでいます。</p>
              <button type="button" className="btn btn--primary" onClick={() => window.location.reload()}>
                対局にもどる
              </button>
            </div>
          )}
        </div>

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

        {rejection && (
          <p className="rejection" role="alert">
            {rejection}
          </p>
        )}

        {!over &&
          (confirmingResign ? (
            <div className="resign-confirm" role="group" aria-label="投了の確認">
              <span>投了しますか。</span>
              <button
                type="button"
                className="btn btn--danger"
                onClick={() => {
                  setConfirmingResign(false);
                  resign();
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

        {over && (
          <div className="match-end">
            {/* **번호는 기록이 다 쓰인 뒤에 온다.** 그 전에는 링크를 안 그린다 —
                없는 판으로 보내는 링크보다 링크가 없는 편이 낫다. */}
            {gameId !== null && (
              <a
                className="btn"
                href={hrefOf({ name: 'review', id: gameId })}
                onClick={(e) => {
                  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                  e.preventDefault();
                  navigate({ name: 'review', id: gameId });
                }}
              >
                この対局を振り返る
              </a>
            )}
            <button type="button" className="btn btn--primary" onClick={() => navigate({ name: 'game' })}>
              対局画面にもどる
            </button>
          </div>
        )}

        {/* **엔진 대국과 같은 컴포넌트다.** 棋譜는 상대가 사람이든 컴퓨터든 같은 것이고,
            갈리는 것은 「누가 뒀나」의 어휘뿐이라 그쪽이 두 값을 다 안다(Kifu). */}
        <Kifu moves={moves} />
      </aside>
    </div>
  );
}

/**
 * 남은 시간 하나. **누구의 것인지는 색이 아니라 글자가 말한다** — 색만으로 가르면
 * 색맹인 사람에게 두 시계가 같아 보이고, 여기 시계는 하나뿐이라 더 그렇다.
 */
function Clock({ leftMs, limitMs, yours }: { leftMs: number; limitMs: number; yours: boolean }) {
  const seconds = Math.ceil(leftMs / 1000);
  // 마지막 10초는 눈에 띄어야 한다. **소리는 안 낸다** — 착수음과 겹치면 무엇이 울린
  // 것인지 모른다.
  const urgent = seconds <= 10;
  return (
    <p className="match-clock" data-urgent={urgent || undefined} data-yours={yours || undefined}>
      <span className="match-clock__who">{yours ? 'あなたの持ち時間' : '相手の持ち時間'}</span>
      <span className="match-clock__value">{seconds}秒</span>
      <span className="sr-only">（一手 {Math.round(limitMs / 1000)}秒）</span>
    </p>
  );
}

/**
 * 결과 한 줄. **`board-view.ts` 의 것과 갈라 둔다** — 저쪽은 승자를 `human`/`engine` 으로
 * 읽는데 여기는 사람이 둘이라 `you`/`opponent` 다. 그리고 여기에만 있는 것이 시간패다.
 */
function matchResultText(snapshot: MatchSnapshot): string | null {
  const won = snapshot.winner === 'you';
  switch (snapshot.status) {
    case 'checkmate':
      return won ? '詰み。あなたの勝ちです。' : '詰み。あなたの負けです。';
    case 'stalemate':
      return won ? '手詰まり。あなたの勝ちです。' : '手詰まり。あなたの負けです。';
    case 'resigned':
      return won ? '相手が投了しました。あなたの勝ちです。' : '投了しました。';
    case 'repetition':
      return '千日手。引き分けです。';
    // **시간패는 누가 넘겼는지를 말한다.** 「時間切れです」만 쓰면 진 쪽이 자기가 넘긴
    // 것인지 상대가 넘긴 것인지를 판에서 되짚어야 한다.
    case 'timeout':
      return won
        ? '相手の持ち時間がなくなりました。あなたの勝ちです。'
        : '持ち時間がなくなりました。あなたの負けです。';
    // **승패를 말하지 않는다.** 서버가 내려간 것이라 두 사람 다 잘못한 것이 없다.
    case 'aborted':
      return 'サーバーの都合でこの対局は中断しました。';
    // 여기도 승패가 없다. **다만 이유가 반대라 문장이 다르다** — 서버 사정이 아니라
    // 아무도 안 둔 것이고, 그것을 「サーバーの都合」로 적으면 없는 고장을 알리게 된다.
    case 'expired':
      return '一手も指されないまま持ち時間が過ぎました。この対局は成立しませんでした。';
    default:
      return null;
  }
}
