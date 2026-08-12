import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';

import { Board, type DropFrom, type Replay } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { Intervention } from './Intervention';
import { Kifu } from './Kifu';
import { Setup } from './Setup';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/libs/game/moves';
import type { Attack, StyleTag } from '@/protocol/game';
import { useGame } from '@/hooks/useGame';
import type { Side } from '@/models/piece';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import { fromIndex, fromUsi, toIndex, toUsi } from '@/models/square';
import { scoreJa } from '@/libs/whatif/branch';
import { useWhatIf } from '@/hooks/useWhatIf';
import { useTagAnnounce } from './hooks';
import { checkRays, lastMoveOf, offsetWithin, rayOf, resultText, type Scene } from '@/libs/game/board-view';

/**
 * 태그가 무엇을 가리키는 말인지 붙이는 라벨.
 *
 * `kind` 가 늘면 타입이 여기서 컴파일을 막는다 — 서버에 축을 추가하고 화면을 안 고치면
 * 실제로 걸렸다.
 */
const KIND_JA: Record<StyleTag['kind'], string> = {
  castle: '囲い',
  formation: '戦法',
  opening: '戦型',
  tesuji: '手筋',
};

/**
 * 상대의 강함 눈금에 붙는 말(`snapshot.opponentStrength`).
 *
 * **다섯 개가 좌우로 대칭이다.** 3이 「아무것도 모르는 상태」이고 거기서 양쪽으로 움직이는
 * 값이라, 한쪽만 이름이 세면 조절이 한 방향으로만 도는 것처럼 읽힌다.
 */
const STRENGTH_JA = ['かなり弱め', '弱め', 'ふつう', '強め', 'かなり強め'];

export function GameScreen() {
  const {
    connection,
    snapshot,
    rejection,
    interventionEpisode,
    play,
    resign,
    dismissRejection,
    start,
    restart,
    whatif,
  } = useGame();
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

  /**
   * 사람과 상대의 쪽. **스냅샷이 말하는 것을 그대로 쓴다** — 한때 화면이 「あなた는 언제나
   * 黑」으로 박혀 있었고, 그 가정이 이 파일 네 자리에 흩어져 있었다(駒台 둘·힌트의 打·회상).
   */
  const me: Side = snapshot?.yourColor === 'w' ? 'white' : 'black';
  const them: Side = me === 'black' ? 'white' : 'black';
  /** 사람이 後手면 판을 뒤집는다. **자기 駒가 아래**여야 판을 읽을 수 있다. */
  const flipped = me === 'white';

  const moves = snapshot?.moves ?? [];
  const last = moves.at(-1);
  const lastMove = useMemo(() => (last ? lastMoveOf(last.usi) : null), [last]);

  // 王手를 받고 있는 玉의 칸. 강조는 판 위에서만 하고 글로는 반복하지 않는다.
  const checked = useMemo(() => {
    if (!live || !snapshot?.inCheck) return null;
    const index = live.squares.findIndex((p) => p?.kind === 'K' && p.side === live.turn);
    return index < 0 ? null : toUsi(fromIndex(index));
  }, [live, snapshot?.inCheck]);

  // 새로 붙은 이름. 판 위에 잠깐 떴다 사라진다.
  const [announced, clearAnnounced] = useTagAnnounce(snapshot?.styleTags, snapshot?.ply ?? 0);

  const intervention = snapshot?.intervention ?? null;
  const intervening = intervention !== null && interventionEpisode > seenEpisode;

  /**
   * 회상의 각 장면을 **재기만 한다.** 수마다의 cp가 여기서 나온다(`evalOf`).
   *
   * **대국 중에 둬 보는 길은 없다.** 한때 이 자리가 분기를 소유해서, 카드가 떠 있는 동안
   * 판을 만지면 대국의 수가 아니라 가정의 수가 됐다 — 물러진 목적이 「다시 두라」인데
   * 판의 기본 뜻이 그것이 아니었고, 카드까지 분기 패널로 바뀌어 읽던 설명이 사라졌다.
   *
   * 그래서 판의 뜻을 하나로 되돌렸다: **개입 중에는 판이 잠기고, 카드를 닫으면 살아난다.**
   * 둬 보는 것은 되짚는 화면(`GameReview`)에만 있다 — 끝난 판이라 무엇을 둬도 아무도
   * 안 잃고, 거기서는 그것이 화면의 본론이다.
   */
  const branch = useWhatIf(whatif, interventionEpisode);

  /** 지금 고를 수 있는 수. **언제나 대국의 것이다** — 판이 한 국면만 그리기 때문이다. */
  const grouped = useMemo(() => groupByOrigin(snapshot?.legalMoves ?? []), [snapshot?.legalMoves]);

  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);

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
    const checksAt = (i: number): Attack[] | undefined =>
      i === 0 ? intervention.retractedChecks : line[i - 1]?.checks;

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
        checks: checkRays(checksAt(i)),
        ray: upcoming ? rayOf(upcoming.usi, upcoming.by) : null,
        next: upcoming ? i : -1,
        // 누가 둔 수인가로 쪽을 정한다. **화면의 위아래가 아니라 대국자로 가른다** —
        // 後手로 두면 「相手 = 白」이 성립하지 않는다.
        dropping:
          parsed?.kind === 'drop' && upcoming
            ? { side: upcoming.by === 'engine' ? them : me, kind: parsed.piece }
            : null,
      });
    }
    return out;
  }, [intervention, me, them]);

  // 같은 수로 또 걸렸을 때도 처음부터 본다. 회차가 오르면 장면도 돌아간다.
  useEffect(() => setScene(0), [interventionEpisode]);

  const walking = intervening && scenes.length > 0;
  const current = walking ? scenes[Math.min(scene, scenes.length - 1)] : null;

  /**
   * 지금 장면까지의 수순 — **물러진 수부터** 세는 한 줄이다.
   *
   * **뿌리가 물러진 수보다 앞으로 못 간다.** §25가 못박은 안전장치인데 여기에는 이유가
   * 하나 더 있다: 그 앞은 곧 **지금 다시 둘 국면**이라, 거기서 최선수를 그리면 대국 중에
   * 답을 알려주는 것이 된다(01-core.md §7).
   */
  const sceneLine = useMemo(() => {
    if (!intervention) return [];
    const line = intervention.refutation ?? [];
    return [intervention.retractedUsi, ...line.slice(0, scene).map((m) => m.usi)];
  }, [intervention, scene]);

  /** 확정된 手数. 물러진 수는 여기 없으므로 이 자리가 곧 분기의 뿌리다. */
  const confirmedPly = snapshot?.moves?.length ?? 0;

  /**
   * 넘겨 보는 장면마다 그 국면을 한 번 잰다. **넘겨 보기와 둬 보기가 같은 장치가 된다** —
   * 그 한 번이 수순 줄의 cp를 채우고, 판 위에 최선수 화살표를 세우고, 그 자리에서
   * 사람이 둘 수 있게 한다.
   */
  useEffect(() => {
    if (!intervening || sceneLine.length === 0) return;
    branch.at(confirmedPly, sceneLine);
  }, [intervening, sceneLine, confirmedPly, branch.at]);

  /**
   * 장면 하나의 값. 길이는 **물러진 수부터 센다**(1이 물러진 수 직후).
   *
   * **상태로 한 벌 더 들고 있지 않는다.** 지나온 자리는 훅의 캐시에 이미 있으므로 거기서
   * 꺼내면 되고, 아직 안 가 본 장면은 빈칸으로 남는다 — 없는 값을 지어내지 않는다.
   */
  const evalAt = useCallback(
    (length: number) => {
      const at = branch.evalOf(length);
      return at ? scoreJa(at.cp, at.mateIn) : '';
    },
    [branch.evalOf],
  );

  /**
   * 갇힘 힌트. **수순을 넘겨 보는 동안에는 안 띄운다** — 그때 판은 물러진 수 뒤의
   * 국면이라, 지금 판에 대한 안내를 그 위에 얹으면 판이 거짓을 말한다.
   */
  const hint = walking ? undefined : snapshot?.hint;
  const hintRay = useMemo(() => {
    if (!hint?.usi) return null;
    const r = rayOf(hint.usi, 'human');
    return r && { ...r, hint: true };
  }, [hint?.usi]);

  /** 회상에서 지금 판에 놓이는 持ち駒. **초록 링은 이쪽만 켠다** — 상대 쪽 채널이다. */
  const recallDrop = current?.dropping ?? null;

  /**
   * 駒台에서 출발하는 화살표의 **자리를 재야 하는 駒.** 회상(打)과 힌트가 같은 장치를
   * 쓰고, 둘은 동시에 뜨지 않는다 — 힌트는 넘겨 보는 동안 꺼진다.
   *
   * **재는 것과 빛나는 것을 갈라 뒀다.** 한때 이 값을 `<Hand dropping>` 에도 그대로
   * 넘겼는데, 그러면 힌트가 `data-dropping` 을 켜서 駒台 駒에 **초록** 링이 붙었다 —
   * 파란 테와 초록 링이 같은 駒에 동시에 걸리고, 초록은 「상대가 무엇을 하는가」다.
   *
   * **여기서 객체를 새로 만들면 안 된다.** 이 값이 아래 `useLayoutEffect` 의 의존성이라,
   * 매 렌더마다 identity가 바뀌면 효과가 다시 돌고 `setDropFrom` 이 또 새 객체를 넣어
   * **무한 루프**가 된다(화면이 통째로 하얘진다). 회상 쪽은 `scenes` 의 useMemo 에서 와서
   * 원래 안정적이었고, 힌트를 얹으면서 그 성질이 깨졌다 — 실제로 打 힌트에서 터졌다.
   */
  const dropping = useMemo(
    () => recallDrop ?? (hint?.drop ? { side: me, kind: hint.drop } : null),
    [recallDrop, hint?.drop, me],
  );

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
    // 같은 값이면 상태를 안 건드린다. **재는 일이 리렌더를 부르고 리렌더가 다시 재게
    // 되는 고리가 이 함수에서 실제로 생겼다** — 새 객체를 넣는 것만으로 identity가
    // 바뀌므로, 위쪽 의존성이 또 흔들리는 날에도 흰 화면 대신 아무 일도 안 일어나게 둔다.
    const next = {
      // 판의 테두리 안쪽이 기준이다 — 화살표가 그 안에 놓이므로.
      x: at.x + piece.offsetWidth / 2 - (of.x + board.clientLeft),
      y: at.y + piece.offsetHeight / 2 - (of.y + board.clientTop),
      sq: square.offsetWidth,
    };
    setDropFrom((prev) => (prev && prev.x === next.x && prev.y === next.y && prev.sq === next.sq ? prev : next));
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

  // 화면에 그리는 판. 넘겨 보는 중이면 그 장면, 아니면 지금 대국의 판이다.
  const board = current?.board ?? live;

  // 아직 아무것도 고르지 않았다. **여기서는 서버에 붙어 있지도 않다**(useGame).
  if (connection === 'idle') return <Setup onStart={start} />;

  if (connection === 'closed') {
    return (
      <div className="notice" role="status">
        <p>接続が切れました。</p>
        <button type="button" className="btn" onClick={newGame}>
          もう一度はじめる
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

  // 서버가 안 보내면 조절이 꺼져 있다는 뜻이다. 기본값으로 메우지 않는다(protocol/game.ts).
  const strength = snapshot.opponentStrength;

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
    // data-flipped 는 **駒의 방향**을 위한 것이다. 판의 자리는 CSS가 아니라 자리 번호로
    // 뒤집혀 있고(Board 의 `seat`), 여기서 도는 것은 글자가 누구를 향하는가뿐이다.
    <div className="game" data-intervening={intervening || undefined} data-flipped={flipped || undefined}>
      {/* 판만 남기고 어두워진다. 클릭은 막지 않는다 — 잠글 것은 이미 판 쪽에서 잠겼고,
          투료까지 못 하게 만들 이유가 없다. */}
      {intervening && <div className="veil" aria-hidden="true" />}

      <div className="game-board">
        {/*
          짜는 순간 판 위에 잠깐 뜬다. **개입 카드와 겹치지 않게 그 아래 겹에 둔다** —
          블런더로 되물러진 순간에 이름까지 함께 뜨면 두 소식이 한 자리를 다툰다.
        */}
        {announced && !intervening && (
          <div className="tag-flash" role="status" key={announced.code} onAnimationEnd={clearAnnounced}>
            <span className="tag-flash__kind">{KIND_JA[announced.kind]}</span>
            <span className="tag-flash__name">{announced.nameJa}</span>
          </div>
        )}

        {/* **위가 상대다.** 어느 색인지가 아니라 누구인지로 자리를 정한다 — 자기 駒台가
            아래에 있어야 판과 같은 방향으로 읽힌다(Board 의 `flipped`). */}
        <Hand
          side={them}
          label="相手"
          pieces={board.hands[them]}
          selected={null}
          playable={new Set()}
          dropping={recallDrop?.side === them ? recallDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === them ? dropping.kind : null}
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
          // 넘겨 보는 중에는 **다음에 올 수**다. 지금 판의 최선수는 여기 안 뜬다 —
          // 대국 중에 짚어 주면 답을 알려주는 것이 된다(01-core.md §7).
          ray={current?.ray ?? null}
          // 대국 화면은 미끄러뜨리지 않는다. 판이 움직이는 자리가 회상의 유령 駒이고,
          // 둘을 같이 켜면 같은 수를 두 방식으로 두 번 그린다.
          motion={null}
          checks={current?.checks ?? []}
          dimmed={walking}
          dropFrom={dropFrom}
          hintSquare={hint?.square ?? null}
          hintRay={hintRay}
          // 회상 중에는 끈다. 그때 판은 물러진 수의 국면이라 지금 국면의 게이지가 거짓말이 된다.
          mateHeat={walking ? 0 : (snapshot.mateHeat ?? 0)}
          me={me}
          flipped={flipped}
          boardRef={boardRef}
          // **개입 중에는 잠긴다.** 카드를 닫으면 살아나고, 그때부터 판을 만지는 것은
          // 언제나 「지금 두는 수」다 — 판의 뜻이 하나여야 한다.
          interactive={playable}
          onSquare={onSquare}
        />

        <Hand
          side={me}
          label="あなた"
          pieces={board.hands[me]}
          selected={origin?.endsWith('*') ? origin : null}
          playable={new Set([...grouped.keys()].filter((o) => o.endsWith('*')))}
          dropping={recallDrop?.side === me ? recallDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === me ? dropping.kind : null}
          hintDrop={hint?.drop ?? null}
          onPick={playable ? pick : () => {}}
        />
      </div>

      <aside className="game-side">
        {/* **이 자리를 다른 패널이 빼앗지 않는다.** 개입이 떠 있는 동안 여기 있는 것은
            카드 하나이고, 카드가 닫히면 아무것도 없다 — 읽고 있던 설명이 조작 중에
            사라지면 무엇을 읽던 중이었는지가 통째로 없어진다. */}
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
              evalAt={evalAt}
              onStep={setScene}
              onDismiss={() => setSeenEpisode(interventionEpisode)}
            />
          )}

        {/* **개입 중에는 차례를 말하지 않는다.** 판이 잠겨 있어서 「あなたの番です」가
            할 일을 가리키지 못하고, 카드가 이미 무엇을 해야 하는지 말하고 있다. */}
        {!intervening && (
          <p className="status" data-tone={statusTone}>
            {statusText}
          </p>
        )}

        {/* 상대의 강함. **개입 중에는 안 그린다** — 이 자리는 카드 하나가 쓴다(위 주석).
            눈금이 조용히 바뀌는 것이 이 기능의 요점이라 숫자도 % 도 쓰지 않는다. */}
        {!intervening && strength !== undefined && (
          <p className="strength" role="status">
            <span className="strength__head">相手の強さ</span>
            <span className="strength__pips" aria-hidden="true">
              {[1, 2, 3, 4, 5].map((n) => (
                <i key={n} data-on={n <= strength || undefined} />
              ))}
            </span>
            <span className="strength__label">{STRENGTH_JA[strength - 1]}</span>
          </p>
        )}

        {/* 고른 진형을 되비춘다. **고르지 않았으면 아무것도 안 쓴다** — 「おまかせ」라고
            적어 두면 없는 설정이 있는 것처럼 자리를 차지한다. 상대의 형태를 알려주는 것이
            아닌 근거는 서버의 `Snapshot.OpponentOpening` 주석. */}
        {!intervening && snapshot.opponentOpening && (
          <p className="opening" role="note">
            <span className="opening__head">相手の戦型</span>
            <span className="opening__name">{snapshot.opponentOpening}</span>
          </p>
        )}

        {!intervening && snapshot.tagHints && snapshot.tagHints.length > 0 && (
          <div className="tag-hint" role="note">
            <p className="tag-hint__head">名前のある手があります</p>
            <ul className="tag-hint__list">
              {snapshot.tagHints.map((t) => (
                <li key={t.code}>
                  <span className="tag-hint__kind">{KIND_JA[t.kind]}</span>
                  <span className="tag-hint__name">{t.nameJa}</span>
                </li>
              ))}
            </ul>
          </div>
        )}

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
