import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';

import { Board, type DropFrom, type LastMove, type Ray, type Replay } from './Board';
import { Hand } from './Hand';
import { Intervention } from './Intervention';
import { Kifu } from './Kifu';
import { WhatIfPanel } from './WhatIfPanel';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/game/moves';
import type { Attack, Player, Snapshot, StyleTag } from '@/game/protocol';
import { useGame } from '@/game/useGame';
import type { Side } from '@/shogi/piece';
import { parseSfen, type Board as BoardModel } from '@/shogi/sfen';
import { fromIndex, fromUsi, toIndex, toUsi } from '@/shogi/square';
import { scoreJa } from '@/whatif/branch';
import { useWhatIf } from '@/whatif/useWhatIf';

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
  /** 이 장면에서 玉을 잡으러 오는 말들. 王手가 아니면 비어 있다. */
  checks: Ray[];
  /** 화살표가 가리키는 수가 수순의 몇 번째인가. 마지막 장면이면 -1. */
  next: number;
  /** 다음 수가 打이면 그 駒. 駒台에서 같이 빛나 화살표의 짝이 된다. */
  dropping: { side: Side; kind: string } | null;
}

/** 王手를 거는 줄. 서버가 준 두 칸을 그대로 옮긴다 — 화면은 누가 王手인지 계산하지 않는다. */
function checkRays(checks: Attack[] | undefined): Ray[] {
  if (!checks?.length) return [];
  const out: Ray[] = [];
  for (const c of checks) {
    try {
      out.push({ from: toIndex(fromUsi(c.from)), to: toIndex(fromUsi(c.to)), by: 'engine', check: true });
    } catch {
      // 못 읽는 좌표로 엉뚱한 선을 긋느니 그 한 줄을 버린다
    }
  }
  return out;
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
 * **`getBoundingClientRect` 를 안 쓴다.** 그쪽은 변형이 끝난 화면 좌표를 주는데, 화살표가
 * 놓이는 자리는 변형 **전**의 배치 좌표다. 개입 때 판을 기울이던 동안에는 그 차이가 곧
 * 화살표가 어긋나는 것이었고(기울기는 뺐다 — index.css), 지금도 판이 어떤 변형을 받든
 * 이 계산은 그대로 맞는다.
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
 * 새로 붙은 이름을 **한 번만** 알린다 — 将棋ウォーズ가 하는 그것이다.
 *
 * 사이드바에 상시 띄워 놓는 것으로 먼저 만들었는데, 브라우저에서 보니 「中飛車」가 상태
 * 문구 아래에 홀로 떠서 **무엇을 가리키는 말인지 알 수 없었다** — 棋譜의 소제목처럼도
 * 읽혔다. 이름 옆에 「戦法」 같은 라벨을 붙여 고칠 수도 있었지만, 그건 사이드바에
 * 설명을 하나 더 얹는 일이다.
 *
 * **사건으로 만들면 설명이 필요 없어진다.** 짜는 순간 판 위에 잠깐 뜨고 사라지면
 * 「방금 내가 이걸 만들었다」가 위치와 타이밍으로 전달된다. 03-frontend.md 의 「평시엔
 * 조용하게」와도 맞는다 — 지나가면 화면에 아무것도 남지 않는다.
 *
 * **회차를 기억한다.** 囲い는 깨졌다가 다시 짜이므로, 코드를 기억하지 않으면 같은
 * 이름이 여러 번 뜬다. 한 대국에서 이름 하나는 한 번이다.
 */
function useTagAnnounce(tags: StyleTag[] | undefined, ply: number): [StyleTag | null, () => void] {
  const seen = useRef(new Set<string>());
  const [showing, setShowing] = useState<StyleTag | null>(null);

  // 첫 수 전에는 알리지 않는다. 새 대국에서 기억을 비우는 자리이기도 하다.
  useEffect(() => {
    if (ply === 0) {
      seen.current = new Set();
      setShowing(null);
    }
  }, [ply]);

  useEffect(() => {
    const fresh = tags?.find((t) => !seen.current.has(t.code));
    if (!fresh || ply === 0) return;

    seen.current.add(fresh.code);
    setShowing(fresh);
  }, [tags, ply]);

  // **언마운트를 타이머로 하지 않는다.** 처음에는 `setTimeout(2200)` 으로 지웠는데,
  // 그러면 길이가 CSS 애니메이션과 **두 벌**이 되어 서로 맞아야 한다. 브라우저에서
  // 실제로 어긋나게 해 보니 요소가 DOM에 남은 채 `opacity: 0` 으로 보이지 않았다 —
  // 에러도 안 나고 화면에도 안 나오는 종류다.
  //
  // 애니메이션이 끝나는 것을 신호로 쓰면 길이의 주인이 CSS 하나가 된다.
  return [showing, () => setShowing(null)];
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
  const { connection, snapshot, rejection, interventionEpisode, play, resign, dismissRejection, restart, whatif } =
    useGame();
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
   * 가정 수순. **개입이 걸린 그 자리에서 둬 본다** — 물러진 수를 실제로 두고, 상대가
   * 어떻게 벌하는지를 넘겨 보는 대신 **직접 응수해 본다**(`useWhatIf`).
   *
   * 되짚는 화면과 **같은 장치**이고 길만 다르다(그 대국의 WebSocket). 회차가 바뀌면
   * 들고 있던 것을 버린다 — 다른 수로 걸린 개입은 다른 가정이다.
   */
  const branch = useWhatIf(whatif, interventionEpisode);

  /**
   * 분기의 자리. **개입이 떠 있는 동안만 산다.**
   *
   * 카드를 닫으면 그 자리는 사라진다 — 남겨 두면 그 국면의 합법수가 **지금 대국의 판**을
   * 통제하게 되고, 판과 규칙이 어긋난다.
   */
  const bNode = intervening ? branch.node : null;

  const grouped = useMemo(
    () => groupByOrigin(bNode?.legalMoves ?? snapshot?.legalMoves ?? []),
    [bNode?.legalMoves, snapshot?.legalMoves],
  );

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
   * 수순을 벗어나 직접 두고 있는가.
   *
   * 줄이 장면의 접두사보다 길면 그렇다. **판이 그때부터 서버가 준 분기의 국면**이고,
   * 카드 자리에는 가정 수순 패널이 선다 — 넘겨 보는 것과 둬 보는 것을 한 화면에 겹치면
   * 어느 판이 사실인지 알 수 없다.
   */
  const exploring = intervening && (branch.node?.line.length ?? 0) > sceneLine.length;
  /** 분기의 판. 못 읽으면 안 그린다 — 그때는 원래 장면이 그대로 남는다. */
  const branchBoard = useMemo<BoardModel | null>(() => {
    if (!exploring || !branch.node) return null;
    try {
      return parseSfen(branch.node.sfen);
    } catch {
      return null;
    }
  }, [exploring, branch.node]);

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
    () => recallDrop ?? (hint?.drop ? { side: 'black' as Side, kind: hint.drop } : null),
    [recallDrop, hint?.drop],
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

  /** 분기에서 방금 둔 수. 흰빛 두 칸으로 짚는다 — **방금 벌어진 것**이다. */
  const branchPlayed = useMemo(() => {
    const played = bNode?.line.at(-1);
    return played ? lastMoveOf(played.usi) : null;
  }, [bNode]);

  /**
   * 분기에서 **수번 쪽의 최선수**. 초록 화살표로 선다.
   *
   * 打은 안 긋는다 — 駒台에서 자리를 재야 하는데 그 측정은 회상 쪽 장치이고, 두 곳이
   * 같은 `dropFrom` 을 다투면 어느 화살표의 출발점인지가 갈린다.
   */
  const branchRay = useMemo<Ray | null>(() => {
    const best = bNode?.candidates[0];
    if (!best) return null;
    const r = rayOf(best.usi, bNode.yourTurn ? 'human' : 'engine');
    return r?.from === null ? null : r;
  }, [bNode]);

  // 화면에 그리는 판. **직접 둬 보는 중이면 그 분기의 국면**, 넘겨 보는 중이면 그 장면,
  // 아니면 지금 대국의 판이다.
  const board = branchBoard ?? current?.board ?? live;

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

  /**
   * 가정 수순을 그 자리에서 둘 수 있는가.
   *
   * **대국 쪽 착수와 동시에 열리지 않는다.** 개입이 떠 있는 동안 판은 잠겨 있고(위),
   * 그때 살아나는 것이 이쪽이다 — 한 판에서 두 종류의 착수가 동시에 되면 사람이 지금
   * 무엇을 두고 있는지 알 수 없다.
   */
  const branchable = !!bNode && bNode.status === 'playing' && !branch.pending && !pending;
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

  /**
   * 고른 수를 어디로 보내나. **대국이면 세션, 가정 수순이면 분기다.**
   *
   * 고르는 장치(`origin`·`pending`)를 하나로 둔 것은 둘이 **동시에 열리지 않기** 때문이다 —
   * 개입이 떠 있으면 대국 판이 잠기고, 카드를 닫으면 분기가 사라진다. 두 벌로 두면 같은
   * 일을 두 곳에서 고치게 된다.
   */
  const sendMove = (usi: string): void => {
    if (branchable) branch.play(usi);
    else play(usi);
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
    sendMove(toUsiMove(origin, to, dest.promote));
    setOrigin(null);
  };

  const onSquare = (usi: string): void => {
    if (!playable && !branchable) return;
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
    sendMove(toUsiMove(pending.origin, pending.to, promote));
    setPending(null);
    setOrigin(null);
  };

  return (
    <div className="game" data-intervening={intervening || undefined}>
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

        <Hand
          side="white"
          label="相手"
          pieces={board.hands.white}
          selected={null}
          playable={new Set()}
          dropping={recallDrop?.side === 'white' ? recallDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === 'white' ? dropping.kind : null}
          onPick={() => {}}
        />

        <Board
          board={board}
          lit={lit}
          selected={origin && !origin.endsWith('*') ? origin : null}
          lastMove={walking || exploring ? null : lastMove}
          // 직접 둬 보는 중에는 그 분기의 王手다. 서버가 짚어 준 것을 그대로 쓴다.
          checked={exploring ? (bNode?.checked ?? null) : walking ? null : checked}
          played={exploring ? branchPlayed : (current?.played ?? null)}
          replay={replay}
          // 넘겨 보는 중에는 **다음에 올 수**, 직접 둬 보는 중에는 **수번 쪽의 최선수**다.
          // 둘 다 「다음에 벌어질 것」이라 같은 초록 화살표를 쓴다.
          ray={exploring ? branchRay : (current?.ray ?? null)}
          // 대국 화면은 미끄러뜨리지 않는다. 판이 움직이는 자리가 회상의 유령 駒이고,
          // 둘을 같이 켜면 같은 수를 두 방식으로 두 번 그린다.
          motion={null}
          checks={exploring ? [] : (current?.checks ?? [])}
          dimmed={walking && !exploring}
          dropFrom={dropFrom}
          hintSquare={hint?.square ?? null}
          hintRay={hintRay}
          // 회상 중에는 끈다. 그때 판은 물러진 수의 국면이라 지금 국면의 게이지가 거짓말이 된다.
          mateHeat={walking ? 0 : (snapshot.mateHeat ?? 0)}
          boardRef={boardRef}
          interactive={playable || branchable}
          onSquare={onSquare}
        />

        <Hand
          side="black"
          label="あなた"
          pieces={board.hands.black}
          selected={origin?.endsWith('*') ? origin : null}
          playable={new Set([...grouped.keys()].filter((o) => o.endsWith('*')))}
          dropping={recallDrop?.side === 'black' ? recallDrop.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          measure={dropping?.side === 'black' ? dropping.kind : null}
          hintDrop={hint?.drop ?? null}
          onPick={playable ? pick : () => {}}
        />
      </div>

      <aside className="game-side">
        {/* **직접 둬 보는 중에는 카드 자리에 분기 패널이 선다.** 둘을 함께 두면 판이 어느
            쪽 것인지 알 수 없고, 무엇보다 카드의 前へ·次へ가 그 판과 어긋난다.

            최선수 목록은 여기서 **안 켠다** — 분기를 물리면 그 자리가 곧 다시 둘 국면이라,
            거기서 최선수를 적어 주면 대국 중에 답을 알려주는 것이 된다(01-core.md §7). */}
        {exploring && bNode ? (
          <WhatIfPanel
            node={bNode}
            pending={branch.pending}
            error={branch.error}
            candidates={false}
            evalOf={branch.evalOf}
            onPlay={(usi) => branch.play(usi)}
            onBack={branch.back}
            onRoot={() => branch.at(confirmedPly, sceneLine)}
          />
        ) : (
          intervening &&
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
              canPlay={branchable}
              onStep={setScene}
              onDismiss={() => setSeenEpisode(interventionEpisode)}
            />
          )
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
