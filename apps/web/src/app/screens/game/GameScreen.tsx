import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';

import { Board, type DropFrom, type Replay } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { Intervention } from './Intervention';
import { Kifu } from './Kifu';
import { Resume } from './Resume';
import { Setup } from './Setup';
import { Summary } from './Summary';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/libs/game/moves';
import type { StyleTag } from '@/protocol/game';
import { useGame } from '@/hooks/useGame';
import { useResumable } from '@/hooks/useResumable';
import type { Side } from '@/models/piece';
import { parseSfen } from '@/models/sfen';
import { fromIndex, fromUsi, toIndex, toUsi } from '@/models/square';
import { scoreJa, type ExploredMove } from '@/libs/whatif/branch';
import { useWhatIf } from '@/hooks/useWhatIf';
import { useTagAnnounce } from './hooks';
import { checkRays, lastMoveOf, offsetWithin, rayOf, resultText } from '@/libs/game/board-view';

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

/** 빈 것을 매 렌더마다 새로 만들지 않는다 — identity가 흔들리면 아래 자식들이 헛돈다. */
const EMPTY_SET: ReadonlySet<string> = new Set();
const noop = (): void => {};

export function GameScreen() {
  const {
    connection,
    setup,
    snapshot,
    rejection,
    summary,
    interventionEpisode,
    play,
    resign,
    dismissRejection,
    start,
    resume,
    restart,
    whatif,
  } = useGame();
  const resumable = useResumable();
  const [origin, setOrigin] = useState<string | null>(null);
  const [pending, setPending] = useState<{ origin: string; to: string } | null>(null);
  const [confirmingResign, setConfirmingResign] = useState(false);
  // 어느 회차까지 봤는가. 서버는 다음 착수까지 개입을 들고 있으므로 「있다」로 판정하면
  // 닫아도 다시 뜬다. 회차를 비교하면 같은 수로 또 걸렸을 때만 다시 연다.
  const [seenEpisode, setSeenEpisode] = useState(0);
  /**
   * 사람이 분기에서 직접 둬 본 수. **자리마다 따로 센다** — 열쇠는 그 자리까지의 줄이다.
   *
   * **화면만 들고 있는다.** 새로고침하면 사라지고, 그것으로 족하다 — 이건 대국의 사실이
   * 아니라 그 사람이 지금 무엇을 궁금해했는가다. 잰 값 자체는 서버가 이미 남겼고
   * (`positions`), 레이팅에는 한 톨도 안 닿는다 — 판정을 지나지 않는 수다.
   */
  const [explored, setExplored] = useState<ReadonlyMap<string, ExploredMove[]>>(new Map());
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
   * 물러진 수 하나가 분기의 **바닥**이다. 그 앞으로는 어느 버튼으로도 못 간다.
   *
   * 바닥 앞은 곧 **지금 다시 둘 국면**이라, 거기서 후보 셋을 그리면 대국 중에 답을
   * 알려주는 것이 된다(01-core.md §7). 서버도 같은 벽을 갖고 있다(ws.go 의 `branchRoot`).
   */
  const retractedUsi = intervention?.retractedUsi;
  const floor = useMemo(() => (retractedUsi ? [retractedUsi] : []), [retractedUsi]);

  /**
   * 물러진 수 뒤의 분기. **되짚는 화면과 같은 장치다**(useWhatIf) — 오가는 길만 다르다.
   *
   * 한때 이 자리가 분기를 소유했다가 통째로 걷어낸 적이 있다. 판을 만지면 옆의 분기
   * 패널이 개입 카드의 자리를 빼앗아 **읽던 설명이 사라졌기** 때문인데, 고쳐야 했던 것은
   * 판이 아니라 그 자리 다툼이었다. 지금은 설명·후보 목록·무르기가 **한 카드 안**에 있고
   * 카드는 무슨 일이 있어도 다른 것으로 바뀌지 않는다(Intervention).
   *
   * 판의 뜻은 여전히 하나다 — **개입 중의 판은 언제나 「그 수를 그대로 뒀다면」이고**,
   * 카드를 닫으면 대국의 판으로 돌아온다.
   */
  const branch = useWhatIf(whatif, interventionEpisode, floor);

  /** 확정된 手数. 물러진 수는 여기 없으므로 이 자리가 곧 분기의 뿌리다. */
  const confirmedPly = snapshot?.moves?.length ?? 0;

  /**
   * 분기의 첫 자리를 연다. **물러진 수부터 깔고 시작한다.**
   *
   * **다시 부를 수 있어야 한다.** 첫 요청이 엔진 고장이나 `busy` 로 튕기면 노드가 영영
   * 안 오고(의존성이 한 회차 내내 그대로다) 카드에는 목록도 무르기도 없이 문구만 남는다 —
   * 그때 사람이 누를 자리가 이것이다.
   */
  const openBranch = useCallback(() => {
    if (!retractedUsi) return;
    branch.at(confirmedPly, [retractedUsi]);
  }, [retractedUsi, confirmedPly, branch.at]);

  /**
   * `interventionEpisode` 가 의존성에 있어야 한다 — 같은 수로 또 걸리면 훅이 들고 있던
   * 것을 버리는데, 그때 `retractedUsi` 는 글자 하나 안 바뀌어서 이 효과가 안 돈다.
   */
  useEffect(() => {
    if (!intervening) return;
    openBranch();
  }, [intervening, interventionEpisode, openBranch]);

  // 회차가 바뀌면 둬 본 것도 버린다. 다른 국면의 수를 그 자리에 얹으면 거짓이 된다.
  useEffect(() => setExplored(new Map()), [interventionEpisode]);

  /**
   * 방금 둬 본 수를 그 자리에 적어 둔다.
   *
   * **값의 관점을 여기서 뒤집는다.** 노드의 cp는 플레이어 관점인데 목록은 **그 수를 둔 쪽
   * 관점**으로 서므로(후보와 같은 자여야 한 줄에 나란히 선다), 상대가 둔 수면 부호가 반대다.
   */
  useEffect(() => {
    const node = branch.node;
    const tried = node?.line.at(-1);
    if (!node || !tried || node.line.length <= 1) return;

    const key = node.line
      .slice(0, -1)
      .map((m) => m.usi)
      .join(' ');
    const flip = tried.by === 'engine';
    const entry: ExploredMove = {
      usi: tried.usi,
      ja: tried.ja,
      cp: node.evalCp === undefined ? undefined : flip ? -node.evalCp : node.evalCp,
      mateIn: node.mateIn === undefined ? undefined : flip ? -node.mateIn : node.mateIn,
    };

    setExplored((prev) => {
      const at = prev.get(key) ?? [];
      if (at.some((e) => e.usi === entry.usi)) return prev; // 같은 수를 두 번 세지 않는다
      const next = new Map(prev);
      next.set(key, [...at, entry]);
      return next;
    });
  }, [branch.node]);

  /** 지금 자리에서 둬 본 수들. 후보 셋 밖의 것만 카드가 줄로 세운다. */
  const exploredHere = useMemo(() => {
    const key = (branch.node?.line ?? []).map((m) => m.usi).join(' ');
    return explored.get(key) ?? [];
  }, [branch.node, explored]);

  /**
   * 물러진 수 자체의 값. **분기의 첫 자리가 그것이다.**
   *
   * **상태로 한 벌 더 들고 있지 않는다.** 그 자리는 훅의 캐시에 이미 있으므로 꺼내면 되고,
   * 아직 못 받았으면 빈칸으로 남는다 — 없는 값을 지어내지 않는다.
   */
  const retractedEval = useMemo(() => {
    const at = branch.evalOf(1);
    return at ? scoreJa(at.cp, at.mateIn) : '';
  }, [branch.evalOf]);

  /**
   * 지금 고를 수 있는 수. 개입 중에는 **분기의 것**이고 아니면 대국의 것이다.
   *
   * 판이 한 국면만 그리므로 이 값도 하나여야 한다 — 둘을 섞으면 판에 없는 駒를 집게 된다.
   */
  const legalMoves = intervening ? (branch.node?.legalMoves ?? []) : (snapshot?.legalMoves ?? []);
  const grouped = useMemo(() => groupByOrigin(legalMoves ?? []), [legalMoves]);

  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  /** 지금 집을 수 있는 駒台의 駒. 어느 쪽 駒台인지는 `handTurn` 이 정한다. */
  const dropOrigins = useMemo(() => new Set([...grouped.keys()].filter((o) => o.endsWith('*'))), [grouped]);

  /**
   * 개입 중에 그리는 판 — **분기의 국면**이다.
   *
   * 노드를 못 받은 동안은 물러진 수를 둔 직후의 국면(`retractedSfen`)이고, 그 둘은
   * **같은 자리**라 값이 와도 판이 깜빡이지 않는다. 국면은 언제나 서버가 준 것을 그대로
   * 그린다 — 화면이 수를 두게 하면 규칙 엔진을 한 벌 더 갖는 것이고, 그건 D2에서
   * 「클라이언트는 규칙을 모른다」로 정해둔 자리다.
   */
  const branchBoard = useMemo(() => {
    if (!intervening || !intervention) return null;
    try {
      return parseSfen(branch.node?.sfen ?? intervention.retractedSfen);
    } catch {
      return null; // 못 읽는 국면으로 판을 그리느니 대국의 판을 그대로 둔다
    }
  }, [intervening, intervention, branch.node]);

  /** 분기의 뿌리에 서 있는가 — 물러진 수 하나만 둔 자리. */
  const atRoot = (branch.node?.line.length ?? 1) <= 1;

  /**
   * 지금 판을 만든 수. 뿌리에서는 물러진 그 수이고, 들어갔으면 분기의 마지막 수다.
   * **실제로 둔 수와 같은 채널로 그린다** — 판 위에서는 어느 쪽이든 「방금 벌어진 것」이고,
   * 이 판이 가정이라는 것은 판 위가 아니라 카드가 말한다.
   */
  const branchPlayed = useMemo(() => {
    if (!intervening || !intervention) return null;
    return lastMoveOf(branch.node?.line.at(-1)?.usi ?? intervention.retractedUsi);
  }, [intervening, intervention, branch.node]);

  /**
   * 판 위의 초록 화살표 — **수번 쪽의 최선수**다.
   *
   * 되짚기와 같은 채널이고(ReviewDetail), 여기서도 「다음에 벌어질 것」이다. **지금 대국의
   * 최선수는 여기 절대 안 뜬다** — 이 화살표가 사는 국면은 되물러서 사라진 자리다.
   */
  const branchRay = useMemo(() => {
    const node = branch.node;
    const best = node?.candidates[0];
    if (!intervening || !node || !best) return null;
    return rayOf(best.usi, node.yourTurn ? 'human' : 'engine');
  }, [intervening, branch.node]);

  /**
   * 王手. **뿌리에서만 「누가 걸고 있는가」까지 안다**(`retractedChecks`). 분기로 들어가면
   * 서버가 玉의 칸 하나만 주므로 거기서는 그것만 그린다 — 없는 것을 지어내지 않는다.
   */
  const branchChecks = useMemo(
    () => (intervening && atRoot ? checkRays(intervention?.retractedChecks) : []),
    [intervening, atRoot, intervention?.retractedChecks],
  );

  /**
   * 갇힘 힌트. **개입 중에는 안 띄운다** — 그때 판은 물러진 수 뒤의 국면이라, 지금 판에
   * 대한 안내를 그 위에 얹으면 판이 거짓을 말한다.
   */
  const hint = intervening ? undefined : snapshot?.hint;
  const hintRay = useMemo(() => {
    if (!hint?.usi) return null;
    const r = rayOf(hint.usi, 'human');
    return r && { ...r, hint: true };
  }, [hint?.usi]);

  /**
   * 초록 화살표가 駒台에서 출발하는가 — **최선수가 打일 때**다. 그때 그 駒가 駒台에서 함께
   * 빛나 화살표의 짝이 된다.
   *
   * **수번 쪽의 駒台다.** 누가 둘 차례인가로 쪽을 정한다 — 화면의 위아래가 아니라
   * 대국자로 가른다. 後手로 두면 「相手 = 白」이 성립하지 않는다.
   */
  const branchDrop = useMemo(() => {
    const node = branch.node;
    const best = node?.candidates[0];
    if (!intervening || !node || !best) return null;
    const move = parseUsi(best.usi);
    if (move?.kind !== 'drop') return null;
    return { side: node.yourTurn ? me : them, kind: move.piece };
  }, [intervening, branch.node, me, them]);

  /**
   * 駒台에서 출발하는 화살표의 **자리를 재야 하는 駒.** 분기의 打과 힌트가 같은 장치를
   * 쓰고, 둘은 동시에 뜨지 않는다 — 힌트는 개입 중에 꺼진다.
   *
   * **재는 것과 빛나는 것을 갈라 뒀다.** 한때 이 값을 `<Hand dropping>` 에도 그대로
   * 넘겼는데, 그러면 힌트가 `data-dropping` 을 켜서 駒台 駒에 **초록** 링이 붙었다 —
   * 파란 테와 초록 링이 같은 駒에 동시에 걸리고, 초록은 「상대가 무엇을 하는가」다.
   *
   * **여기서 객체를 새로 만들면 안 된다.** 이 값이 아래 `useLayoutEffect` 의 의존성이라,
   * 매 렌더마다 identity가 바뀌면 효과가 다시 돌고 `setDropFrom` 이 또 새 객체를 넣어
   * **무한 루프**가 된다(화면이 통째로 하얘진다). 그래서 양쪽 다 useMemo 를 지난다 —
   * 실제로 打 힌트에서 터졌다.
   */
  const dropping = useMemo(
    () => branchDrop ?? (hint?.drop ? { side: me, kind: hint.drop } : null),
    [branchDrop, hint?.drop, me],
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
   * 판은 이미 그 수를 둔 국면이므로 **유령 駒는 뿌리에서 한 번만** 난다 — 분기로 한 수라도
   * 들어가면 그 판은 물러진 수의 국면이 아니다. 어느 駒였는지는 되돌아온 판
   * (`snapshot.sfen`)의 출발 칸에서 읽는다 — 성한 수라면 도착 칸에는 이미 성한 駒가 서 있어서,
   * 날아가는 것이 무엇이었는지가 거기엔 없다.
   */
  const replay = useMemo<Replay | null>(() => {
    if (!intervening || !intervention || !live || !atRoot) return null;

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
  }, [intervening, intervention, live, atRoot]);

  // 화면에 그리는 판. 개입 중이면 분기의 국면, 아니면 지금 대국의 판이다.
  const board = branchBoard ?? live;

  // 아직 아무것도 고르지 않았다. **여기서는 서버에 붙어 있지도 않다**(useGame).
  if (connection === 'idle') {
    // 두다 만 판이 있으면 **고르는 화면보다 먼저 묻는다.** 선후공부터 다시 고르게 하면
    // 그 판은 사람이 존재를 모르는 채로 사라진다.
    if (resumable.game) {
      const unfinished = resumable.game;
      return (
        <Resume
          game={unfinished}
          onResume={() => {
            resumable.taken();
            resume(unfinished);
          }}
          onDecline={resumable.decline}
        />
      );
    }
    return <Setup initial={setup} onStart={start} />;
  }

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

  /**
   * 판을 만질 수 있는가. **개입 중에도 만질 수 있고, 그때 판의 뜻은 하나다** — 「그 수를
   * 그대로 뒀다면」이다(03-frontend.md §2의 「시간을 멈춘다」는 대국의 시계 이야기다).
   *
   * 분기 쪽은 **어느 쪽 차례든** 둘 수 있다. 「상대라면 어떻게 둘까」를 직접 둬 보는 것이
   * 이 자리의 내용이고, 되짚기가 이미 그렇게 돈다.
   *
   * 판정 중(`judging`)에는 서버가 이미 `yourTurn`을 내려두므로 대국 쪽은 여기서 더 할 것이 없다.
   */
  const playable = intervening
    ? !!branch.node && branch.node.status === 'playing' && !branch.pending && !pending
    : snapshot.yourTurn && snapshot.status === 'playing' && !pending;

  /** 지금 집을 수 있는 駒台. 대국에서는 언제나 내 쪽이고, 분기에서는 **수번 쪽**이다. */
  const handTurn: Side = intervening ? (branch.node?.yourTurn ? me : them) : me;

  /** 한 수 둔다. **개입 중이면 대국이 아니라 분기로 간다** — 판의 뜻이 그것 하나다. */
  const commitMove = (usi: string): void => {
    if (intervening) branch.play(usi);
    else play(usi);
  };

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
    commitMove(toUsiMove(origin, to, dest.promote));
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
    commitMove(toUsiMove(pending.origin, pending.to, promote));
    setPending(null);
    setOrigin(null);
  };

  /** 목록에서 골라 두는 길. 판 위에서 두는 것과 **같은 한 수**다 — 고르는 자리만 다르다. */
  const playFromList = (usi: string): void => {
    setOrigin(null);
    branch.play(usi);
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
          selected={handTurn === them && origin?.endsWith('*') ? origin : null}
          // **분기에서는 상대의 駒台도 집는다.** 「상대라면 어떻게 둘까」를 직접 둬 보는 것이
          // 이 자리의 내용이고, 그 수가 打일 수 있다. 대국 중에는 여기가 언제나 비어 있다.
          playable={playable && handTurn === them ? dropOrigins : EMPTY_SET}
          dropping={branchDrop?.side === them ? branchDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === them ? dropping.kind : null}
          onPick={playable && handTurn === them ? pick : noop}
        />

        <Board
          board={board}
          lit={lit}
          selected={origin && !origin.endsWith('*') ? origin : null}
          lastMove={intervening ? null : lastMove}
          // 뿌리에서는 王手의 **줄**을 긋고 있으므로(`branchChecks`) 玉의 칸까지 켜면 같은
          // 사실이 두 채널로 나간다. 분기로 들어가면 서버가 그 칸만 준다.
          checked={intervening ? (atRoot ? null : (branch.node?.checked ?? null)) : checked}
          played={branchPlayed}
          replay={replay}
          // 개입 중에는 **수번 쪽의 최선수**다. 그 국면은 되물러서 사라진 자리라 「지금
          // 이렇게 두라」가 아니다 — 지금 판의 최선수는 여기 절대 안 뜬다(01-core.md §7).
          ray={branchRay}
          // 대국 화면은 미끄러뜨리지 않는다. 판이 움직이는 자리가 유령 駒이고,
          // 둘을 같이 켜면 같은 수를 두 방식으로 두 번 그린다.
          motion={null}
          checks={branchChecks}
          dimmed={intervening}
          dropFrom={dropFrom}
          hintSquare={hint?.square ?? null}
          hintRay={hintRay}
          // 개입 중에는 끈다. 그때 판은 물러진 수 뒤의 국면이라 지금 국면의 게이지가 거짓말이 된다.
          mateHeat={intervening ? 0 : (snapshot.mateHeat ?? 0)}
          me={me}
          flipped={flipped}
          boardRef={boardRef}
          // **개입 중에도 만질 수 있다.** 그때 판의 뜻은 하나다 — 「그 수를 그대로 뒀다면」.
          // 카드를 닫으면 대국의 판으로 돌아온다(`playable`).
          interactive={playable}
          onSquare={onSquare}
        />

        <Hand
          side={me}
          label="あなた"
          pieces={board.hands[me]}
          selected={handTurn === me && origin?.endsWith('*') ? origin : null}
          playable={playable && handTurn === me ? dropOrigins : EMPTY_SET}
          dropping={branchDrop?.side === me ? branchDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === me ? dropping.kind : null}
          hintDrop={hint?.drop ?? null}
          onPick={playable && handTurn === me ? pick : noop}
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
              node={branch.node}
              pending={branch.pending}
              error={branch.error}
              retractedEval={retractedEval}
              explored={exploredHere}
              playable={playable}
              onPlay={playFromList}
              onBack={branch.back}
              onRoot={branch.toRoot}
              onRetry={openBranch}
              // **고르던 것을 전부 버리고 닫는다.** 그것들은 분기의 국면에 대해 고른 것이라,
              // 남겨 두면 대국의 판 위에서 뜻이 달라진다 — 成りますか를 띄운 채로 닫으면
              // 「成る」가 분기의 수를 **진짜 수로** 둬 버린다.
              onDismiss={() => {
                setOrigin(null);
                setPending(null);
                setSeenEpisode(interventionEpisode);
              }}
            />
          )}

        {/* **개입 중에는 차례도 강함도 진형도 말하지 않는다.** 이 자리는 카드 하나가 쓴다
            (위 주석). 판이 잠겨 있어서 「あなたの番です」가 할 일을 가리키지도 못한다.

            셋이 한 카드다 — 전부 「지금 이 판이 어떤 상태인가」이고, 따로 떼어 놓으면
            판 옆에 문단 셋이 흩어져 어느 것이 제목인지가 없어진다. */}
        {!intervening && (
          <div className="game-state">
            <p className="status" data-tone={statusTone}>
              {statusText}
            </p>

            {/* 상대의 강함. 눈금이 조용히 바뀌는 것이 이 기능의 요점이라 숫자도 % 도 쓰지 않는다. */}
            {strength !== undefined && (
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
            {snapshot.opponentOpening && (
              <p className="opening" role="note">
                <span className="opening__head">相手の戦型</span>
                <span className="opening__name">{snapshot.opponentOpening}</span>
              </p>
            )}
          </div>
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

        {/* 총평은 **기보 아래·「もう一局」 위**다. 판이 끝난 뒤 읽는 순서가 결과 → 기보 →
            무엇을 배웠나 → 다음 판이고, 버튼을 위에 두면 읽기 전에 눌러 버린다.

            개입 중에는 안 그린다 — 이 자리는 카드 하나가 쓴다(위 주석). 다만 판이 끝난
            뒤에는 개입이 뜰 일이 없어서 실제로는 겹치지 않는다. */}
        {!intervening && snapshot.status !== 'playing' && <Summary summary={summary} />}

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
