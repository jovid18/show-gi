import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';

import { Board, type DropFrom, type LastMove, type Ray } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { EvalGraph } from './EvalGraph';
import { MoveOptions } from './MoveOptions';
import { WhatIfPanel } from './WhatIfPanel';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/libs/game/moves';
import { offsetWithin } from '@/libs/game/board-view';
import { dateJa, resultJa } from '@/libs/review/labels';
import type { GameDetail, ReviewMove } from '@/protocol/review';
import type { WhatIfNode } from '@/protocol/whatif';
import { useEngineReady } from '@/hooks/useReview';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import { fromUsi, toIndex, type Motion } from '@/models/square';
import { branchMotion, evalText, stepMotion } from '@/libs/whatif/branch';
import { httpSend } from '@/libs/whatif/http';
import { useWhatIf } from '@/hooks/useWhatIf';
import { useMoveEvals } from '@/hooks/useMoveEvals';

/**
 * 한 판을 되짚는다.
 *
 * **판은 언제나 「지금 고른 手数까지 둔 뒤」다.** 그 국면은 서버가 준 SFEN 그대로이고
 * 화면은 수를 두지 않는다 — 대국에서 정한 것과 같은 자리다(화면은 규칙을 모른다).
 *
 * **어느 국면에서든 그 자리에서 둬 볼 수 있다.** 手数에 멈추면 서버가 그 국면을 한 번 재고,
 * 그때부터 판이 살아 있다 — 사람 차례든 상대 차례든 그 쪽 駒를 집을 수 있고, 두면 그 수의
 * cp가 붙고 **상대의 최선수가 초록 화살표**로 선다(useWhatIf).
 */
interface ReviewDetailProps {
  game: GameDetail;
  onBack: () => void;
}

/**
 * 手数에 멈춘 뒤 국면을 물어보기까지 기다리는 시간.
 *
 * **넘기는 중에는 안 묻는다.** ▶ 를 연달아 누르거나 → 를 누른 채로 두면 지나가는 手数마다
 * 깊이 12 탐색이 걸리고, 그건 엔진 풀을 대국과 나눠 쓰는 구조에서 남의 대국을 세우는 일이다.
 */
const SETTLE_MS = 350;

/** 판 위에 그을 두 칸. 못 읽는 좌표면 안 그린다 — 엉뚱한 칸을 칠하느니 비운다. */
function squaresOf(usi: string): LastMove | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    return {
      from: move.kind === 'drop' ? null : toIndex(fromUsi(move.from)),
      to: toIndex(fromUsi(move.to)),
    };
  } catch {
    return null;
  }
}

/**
 * 棋譜를 **2단**으로 놓는다 — ▲과 △이 한 줄에 선다.
 *
 * 실제 기보가 그렇게 적히고, 세로 길이가 절반이 되어 판을 밀어내지 않는다. **手数의 홀짝으로
 * 자리를 정한다** — 中盤에서 시작하는 판(games.start_sfen)에서는 1手目가 後手일 수 있지만,
 * 그때도 「같은 手数가 같은 열에 선다」가 유지되는 쪽이 읽기 쉽다.
 *
 * 구멍이 난 기보(큐가 넘쳐 한 수가 빠졌다)에서도 자리가 밀리지 않는다 — 홀짝이 자리를
 * 정하므로 빠진 칸이 빈 칸으로 남는다.
 */
function pairRows(moves: readonly ReviewMove[]): (ReviewMove | null)[][] {
  const rows: (ReviewMove | null)[][] = [];
  for (const m of moves) {
    const column = m.ply % 2 === 1 ? 0 : 1;
    const last = rows.at(-1);
    if (!last || last[column]) rows.push(column === 0 ? [m, null] : [null, m]);
    else last[column] = m;
  }
  return rows;
}

export function ReviewDetail({ game, onBack }: ReviewDetailProps) {
  /** 지금 보고 있는 手数. 0이면 시작 국면이다. */
  const [ply, setPly] = useState(0);
  /** 지금 나는 움직임. `id`가 바뀔 때마다 그 칸에서 다시 난다. */
  const [motion, setMotion] = useState<Motion | null>(null);
  const motionId = useRef(0);
  /** 분기에서 고른 駒. */
  const [origin, setOrigin] = useState<string | null>(null);
  /** 成/不成 둘 다 되는 수. 물어보는 동안 다른 수를 못 두게 잡아 둔다. */
  const [promoting, setPromoting] = useState<{ origin: string; to: string } | null>(null);
  /**
   * 기보가 펼쳐져 있는가. **닫힌 것이 기본이다.**
   *
   * 이 목록은 「어디로 갈까」를 고르는 자리이고, 골랐으면 닫힌다 — 판을 보러 온 화면에서
   * 목록이 계속 자리를 잡고 있으면 판이 그만큼 작아진다.
   */
  const [kifuOpen, setKifuOpen] = useState(false);
  /** 打 화살표의 출발점 — 駒台에 놓인 그 駒의 실제 자리. 칸 산수 밖이라 **재야** 한다. */
  const [dropFrom, setDropFrom] = useState<DropFrom | null>(null);
  const dropPieceRef = useRef<HTMLButtonElement | null>(null);
  const boardRef = useRef<HTMLDivElement>(null);

  const engineReady = useEngineReady();
  const whatif = useWhatIf(httpSend(game.id), game.id);
  const { node, pending, branching, at, play, back, toRoot, clear } = whatif;

  /**
   * 지금 手数의 국면인가.
   *
   * **다른 手数의 노드를 판에 얹지 않는다.** 넘기는 중에 앞 요청이 늦게 오면 그 국면의
   * 합법수로 이 판을 두게 되고, 그러면 판과 규칙이 어긋난다.
   */
  const active = node && node.basePly === ply ? node : null;

  /**
   * 옆 패널이 그리고 있는 노드. **기다리는 동안 직전 것을 그대로 둔다.**
   *
   * 판에 두는 것은 `active` 뿐이다(위) — 이쪽은 **그리기 전용**이라 다른 手数의 것이어도
   * 규칙이 어긋날 자리가 없다. 그래서 「값이 오면 갈아 끼운다」가 성립한다.
   */
  const shownRef = useRef<WhatIfNode | null>(null);
  if (active) shownRef.current = active;
  const shown = shownRef.current;

  /** 물러진 수 중 값이 저장돼 있지 않은 것들. 그 자리를 다시 재서 채운다. */
  const unmeasured = useMemo(
    () =>
      game.interventions
        .filter((iv) => iv.ply === ply + 1 && !!iv.retractedUsi && iv.afterCp === undefined)
        .map((iv) => iv.retractedUsi as string),
    [game.interventions, ply],
  );
  const measured = useMoveEvals(game.id, ply, unmeasured);
  /** 지금 분기가 시작된 수. 그 줄이 목록에서 열린다. */
  const chosen = shown?.line[0]?.usi ?? null;

  const last = game.moves.length;

  /** 다음 움직임의 열쇠. 올려 두면 같은 칸에 두 번 들어와도 다시 난다(되잡기). */
  const nextMotionId = useCallback(() => ++motionId.current, []);

  const goto = useCallback(
    (next: number) => {
      const target = Math.min(Math.max(next, 0), last);
      // 手数를 옮기면 회상도 분기도 끝난다. **둘 다 그 국면에서만 사실이다.**
      setOrigin(null);
      setPromoting(null);
      clear();
      // **분기에서 나오는 길에는 움직임을 안 그린다.** 판이 다른 줄에서 통째로 갈아치워지는
      // 것이라, 그 위에서 駒 하나가 미끄러지면 「이 한 수로 이렇게 됐다」는 거짓말이 된다.
      setMotion(branching ? null : stepMotion(game.moves, ply, target, nextMotionId()));
      setPly(target);
    },
    [ply, last, game.moves, branching, clear, nextMotionId],
  );

  /**
   * 목록에서 골라 그 手数로 간다. **고르면 닫힌다.**
   *
   * 목록은 「어디로 갈까」를 묻는 자리이고 답을 받으면 할 일이 끝난다 — 열어 둔 채로 두면
   * 방금 고른 국면을 그 목록이 가린다.
   */
  const jumpTo = useCallback(
    (next: number) => {
      goto(next);
      setKifuOpen(false);
    },
    [goto],
  );

  /**
   * 그래프를 누르면 그 手数로 가고, **거기에 물러진 수가 있으면 그것을 꺼낸다.**
   *
   * 점이 없는 자리를 누르면 목록을 닫는다 — 남겨 두면 지금 보고 있는 판과 다른 국면의
   * 개입이 옆에 떠 있게 된다.
   */
  /**
   * 그래프를 누르면 그 手数로 간다.
   *
   * 한때 「빨간 점이면 개입 목록을 꺼낸다」가 여기 붙어 있었다. 목록이 이제 그 국면의
   * **둘 수 있었던 수 전부**라 언제나 서 있고, 꺼낼 것이 없다.
   */
  const onGraphPick = goto;

  const playBranch = useCallback(
    (usi: string) => {
      setOrigin(null);
      setPromoting(null);
      play(usi);
    },
    [play],
  );

  /**
   * 手数에 멈추면 그 국면을 한 번 잰다.
   *
   * 이 한 번이 셋을 준다 — 지금 국면의 값, 그 자리에서 둘 수 있는 수(자유 착수가 이것을
   * 기다린다), 그리고 최선수 셋. 서버가 이미 잰 국면이면 왕복도 탐색도 없다(positions).
   */
  useEffect(() => {
    if (branching || engineReady === false) return;
    const timer = setTimeout(() => at(ply), SETTLE_MS);
    return () => clearTimeout(timer);
  }, [ply, branching, engineReady, at]);

  // 분기가 한 걸음 나아가면 그 수가 판 위에서 움직인다. **판이 통째로 바뀌면 초심자는
  // 무엇이 변했는지 못 본다**(03-frontend.md §3) — 여기가 그 문장이 걸린 자리다.
  //
  // **`useLayoutEffect` 여야 한다.** `useEffect` 는 페인트 **뒤에** 도는데, 그러면 순서가
  // 이렇게 된다: 새 판이 그려져 駒가 도착 칸에 한 번 뜨고 → 그 다음 프레임에 미끄러짐이
  // 붙어 駒가 출발 칸으로 되돌아가 다시 온다. **한 수에 駒가 두 번 움직인다.**
  // 페인트 전에 붙이면 첫 그림부터 駒가 출발 칸에 있고, 움직임은 한 번이다.
  useLayoutEffect(() => {
    if (!node?.line.length) return;
    setMotion(branchMotion(node, nextMotionId()));
  }, [node, nextMotionId]);

  // ← → 로 한 수씩, Home·End 로 끝까지. 넘겨 보는 화면에서 손이 제일 먼저 가는 자리다.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // 글자를 넣고 있는 중이면 그 키는 그쪽 것이다.
      if (e.target instanceof HTMLInputElement) return;
      switch (e.key) {
        case 'ArrowLeft':
          goto(ply - 1);
          break;
        case 'ArrowRight':
          goto(ply + 1);
          break;
        case 'Home':
          goto(0);
          break;
        case 'End':
          goto(last);
          break;
        default:
          return;
      }
      e.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [goto, ply, last]);

  // 분기에 들어가 있으면 그 국면이고, 아니면 실제로 둔 판이다. **분기의 첫 수를 두기 전에는
  // 둘이 같은 국면**이라, 노드를 기다리는 동안 판이 비거나 깜빡이지 않는다.
  const sfen = branching && active ? active.sfen : ply === 0 ? game.startSfen : (game.moves[ply - 1]?.sfen ?? '');
  const board = useMemo<BoardModel | null>(() => {
    if (!sfen) return null;
    try {
      return parseSfen(sfen);
    } catch {
      return null; // 못 읽는 국면으로 판을 그리느니 그 자리를 비운다
    }
  }, [sfen]);

  const current = ply === 0 ? null : (game.moves[ply - 1] ?? null);

  /**
   * 이 판을 만든 수. 회상 중에는 안 짚는다 — 그때 주인공은 물러진 수다.
   *
   * 분기에서는 그 줄의 마지막 수다. **실제로 둔 수와 같은 채널로 그린다** — 판 위에서는
   * 어느 쪽이든 「방금 벌어진 것」이고, 이 판이 가정이라는 것은 판 위가 아니라 옆에서 말한다.
   */
  const lastMove = useMemo(() => {
    if (branching && active) {
      const played = active.line.at(-1);
      return played ? squaresOf(played.usi) : null;
    }
    return current ? squaresOf(current.usi) : null;
  }, [branching, active, current]);

  /**
   * 판 위의 화살표. **회상에서는 물러진 수**, **분기에서는 수번 쪽의 최선수**다.
   *
   * **확정된 판 위에는 안 긋는다.** 한때 手数에 멈추기만 하면 그어졌는데, 그건 이 화면이
   * 「둬 보면 최선수가 선다」로 설계된 것과 어긋난다(03-frontend.md §3) — 넘겨 보는 것만으로
   * 답이 판에 그려지면 스스로 찾을 자리가 없어진다.
   *
   * 그리고 두 뜻이 **같은 초록 화살표**를 쓴다. 회상의 것은 「네가 두려던 나쁜 수」이고
   * 분기의 것은 「지금 최선은 무엇인가」라 정반대인데, 모양이 같으니 어느 쪽인지는 **판이
   * 아니라 옆 패널**이 말한다. 동시에 안 뜨는 것으로는 그 혼동이 안 없어진다 — 그래서
   * 뜨는 자리를 줄여 「지금 무엇을 보고 있나」가 분명한 때만 긋는다.
   */
  const ray = useMemo<Ray | null>(() => {
    if (!branching) return null;
    const best = active?.candidates[0];
    if (!best) return null;
    const squares = squaresOf(best.usi);
    if (!squares) return null;
    // **打도 긋는다.** 판 위에 출발 칸이 없어서 駒台에서 자리를 재야 하고, 아래에서 잰다
    // (`dropFrom`). 안 그리면 최선수가 打인 국면에서만 화살표가 통째로 사라진다.
    return { from: squares.from, to: squares.to, by: active.yourTurn ? 'human' : 'engine' };
  }, [branching, active]);

  /**
   * 한 번이라도 막힌 手数. 기보 줄에 표식을 붙이는 데 쓴다.
   *
   * **몇 번인지는 안 센다.** 같은 국면에서 여러 번 물러지는 일이 실제로 있지만, 그 횟수는
   * 아래 개입 목록이 줄로 보여준다.
   */
  const stopped = useMemo(() => new Set(game.interventions.map((iv) => iv.ply)), [game.interventions]);

  const humanLabel = game.myColor === 'b' ? 'あなた' : '相手';
  const whiteLabel = game.myColor === 'b' ? '相手' : 'あなた';

  /**
   * 지금 그 자리에서 둘 수 있는 수.
   *
   * **목록에 있으면 둘 수 있고 없으면 못 둔다.** 二歩도 打ち歩詰め도 여기서 안 본다 —
   * 애초에 서버가 안 보낸다(game/moves.ts). 회상 중에는 잠근다: 그때 판은 물러진 수의
   * 국면이고 노드는 그 한 수 앞의 것이라, 둘이 어긋난 채로 두게 된다.
   */
  const legal = active?.legalMoves ?? [];
  const grouped = useMemo(() => groupByOrigin(legal), [legal]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const playable = !!active && active.status === 'playing' && !pending && !promoting;
  /** 지금 수번의 駒台만 집을 수 있다. 판을 안 뒤집으므로 여기서 어느 쪽인지가 갈린다. */
  const handSide = active?.turn === 'b' ? 'black' : 'white';
  const droppable = useMemo(
    () => (playable ? new Set([...grouped.keys()].filter((o) => o.endsWith('*'))) : new Set<string>()),
    [playable, grouped],
  );

  const onSquare = (usi: string): void => {
    if (!playable) return;
    if (origin && lit.has(usi)) {
      const dest = destinations.find((d) => d.to === usi);
      if (!dest) return;
      // 성/불성이 둘 다 가능할 때만 묻는다. 강제 승격은 목록에 성만 들어 있다.
      if (dest.plain && dest.promote) {
        setPromoting({ origin, to: usi });
        return;
      }
      playBranch(toUsiMove(origin, usi, dest.promote));
      return;
    }
    setOrigin(grouped.has(usi) && usi !== origin ? usi : null);
  };

  const pickHand = (next: string): void => {
    if (!playable) return;
    setOrigin(next === origin ? null : next);
  };

  /**
   * 고른 줄을 목록 안에서 보이게 한다.
   *
   * **`scrollIntoView` 를 안 쓴다.** 그쪽은 목록이 아직 넘치지 않으면 **페이지를** 스크롤하고,
   * 좁은 화면에서는 그때 판이 시야 밖으로 밀린다. 목록의 scrollTop 만 움직이면 페이지는
   * 가만히 있다. 이미 보이는 줄은 건드리지 않는다 — 넘길 때마다 목록이 뛰면 못 읽는다.
   */
  const kifuRef = useRef<HTMLOListElement>(null);
  useEffect(() => {
    const list = kifuRef.current;
    const row = list?.querySelector<HTMLElement>('[data-selected]');
    if (!list || !row) return;

    const top = row.offsetTop;
    const bottom = top + row.offsetHeight;
    if (top < list.scrollTop) list.scrollTop = top;
    else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight;
    // **여는 것도 신호다.** 목록은 접혀 있다가 열리므로, `ply` 만 보면 109수 판을 열었을 때
    // 맨 위가 보이고 지금 자리는 한참 아래에 있다.
  }, [ply, kifuOpen]);

  /**
   * 지금 화살표가 駒台에서 출발하는가. 그렇다면 어느 쪽의 무슨 駒인가.
   *
   * **identity가 안정적이어야 한다.** 매 렌더마다 새 객체가 나오면 아래 효과가 다시 돌고
   * `setDropFrom` 이 또 새 객체를 넣어 무한 루프가 된다 — 대국 화면에서 실제로 그렇게
   * 화면이 하얘졌다(GameScreen 의 `dropping` 주석).
   */
  const dropping = useMemo(() => {
    if (!ray || ray.from !== null) return null;
    const move = parseUsi(active?.candidates[0]?.usi ?? '');
    if (move?.kind !== 'drop') return null;
    // 打은 **수번 측 駒台**에서 나온다. `handSide` 가 이미 그 쪽이다.
    return { side: handSide, kind: move.piece };
  }, [ray, active, handSide]);

  // 화면 폭이 바뀌면 `--sq` 가 따라 변하므로 그때마다 다시 잰다.
  const measureDrop = useCallback(() => {
    const grid = boardRef.current;
    const piece = dropPieceRef.current;
    const stage = grid?.closest('.game-board');
    if (!grid || !piece || !(stage instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    const pieceAt = offsetWithin(piece, stage);
    const gridAt = offsetWithin(grid, stage);
    const square = grid.firstElementChild;
    if (!pieceAt || !gridAt || !(square instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    // 판의 테두리 안쪽이 기준이다 — 화살표가 그 안에 놓이므로.
    const next = {
      x: pieceAt.x + piece.offsetWidth / 2 - (gridAt.x + grid.clientLeft),
      y: pieceAt.y + piece.offsetHeight / 2 - (gridAt.y + grid.clientTop),
      sq: square.offsetWidth,
    };
    // 같은 값이면 상태를 안 건드린다 — 재는 일이 리렌더를 부르고 리렌더가 다시 재는 고리를 끊는다.
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

  const rows = useMemo(() => pairRows(game.moves), [game.moves]);

  /**
   * 이동 바 가운데 칸에 적히는 말. **手数가 아니라 수의 이름이다.**
   *
   * 「15 / 109」는 어디쯤인지만 말하고 **거기가 무슨 수였나**를 말하지 않는다. 되짚는 사람이
   * 찾는 것은 후자다. 총 手数는 이 옆 제목이 든다(`棋譜 109手`).
   *
   * 분기에 들어가 있으면 판이 그 手数의 국면이 아니므로 그렇다고 적는다 — 안 적으면
   * 확정된 수의 이름이 남의 판 위에 떠 있게 된다.
   */
  const jumpLabel = useMemo(() => {
    const here = ply === 0 ? '開始局面' : `${ply} ${game.moves[ply - 1]?.ja ?? ''}`.trim();
    return branching ? `${here} · もしも` : here;
  }, [ply, game.moves, branching]);

  return (
    <div className="game review">
      <div className="game-board">
        {/* **평가치 궤적이 곧 이동 장치다.** 「어디서 무너졌나」를 목록으로 읽게 하는 대신
            한 장으로 보여주고 거기를 눌러 돌아가게 한다 — 빨간 점이 물러진 수가 있던 자리다. */}
        <section className="review-panel review-graph-panel" aria-label="評価値">
          <EvalGraph game={game} ply={ply} whatif={whatif} onPick={onGraphPick} />
        </section>

        {/* **이 판은 실제로 벌어진 일이 아니다.** 옆 패널의 제목만으로는 판을 보는 동안
            그 사실이 안 남는다 — 되짚기와 같은 판·같은 駒台라 더 그렇다. */}
        {branching && (
          <p className="review-branch-badge" role="status">
            もしもの局面
          </p>
        )}

        <Hand
          side="white"
          label={whiteLabel}
          pieces={board?.hands.white ?? {}}
          selected={handSide === 'white' && origin?.endsWith('*') ? origin : null}
          playable={handSide === 'white' ? droppable : new Set()}
          measure={dropping?.side === 'white' ? dropping.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          onPick={handSide === 'white' ? pickHand : () => {}}
        />

        {board ? (
          <Board
            board={board}
            lit={lit}
            selected={origin && !origin.endsWith('*') ? origin : null}
            lastMove={lastMove}
            checked={branching && active ? (active.checked ?? null) : (current?.checked ?? null)}
            played={null}
            replay={null}
            ray={ray}
            motion={motion}
            checks={[]}
            // **되짚기에서는 판을 탈색하지 않는다.** 탈색은 「지금이 아니다」를 말하는 장치인데
            // (index.css `.board-tint`), 이 화면은 **전부가 지금이 아니다** — 그 안에서 한 국면만
            // 낮추면 무엇과 구별되는지가 없다. 대국 화면에는 남는다: 거기서는 살아 있는 판과
            // 회상이 같은 자리를 쓴다.
            dimmed={false}
            dropFrom={dropFrom}
            boardRef={boardRef}
            hintSquare={null}
            hintRay={null}
            mateHeat={0}
            interactive={playable}
            onSquare={onSquare}
          />
        ) : (
          <p className="review-broken">この手からは局面を再現できません。</p>
        )}

        <Hand
          side="black"
          label={humanLabel}
          pieces={board?.hands.black ?? {}}
          selected={handSide === 'black' && origin?.endsWith('*') ? origin : null}
          playable={handSide === 'black' ? droppable : new Set()}
          measure={dropping?.side === 'black' ? dropping.kind : null}
          droppingRef={(el) => {
            dropPieceRef.current = el;
          }}
          onPick={handSide === 'black' ? pickHand : () => {}}
        />
        {/* **이동과 기보가 한 컨트롤이다**(将棋ウォーズ). 슬라이더는 뺐다 — 「지금 어디인가」를
            말하는 자리가 셋이었고, 그중 하나만 남긴 것이 아래 가운데 칸이다. */}
        {/* 제목 줄을 두지 않는다 — `棋譜 167手` 는 숫자 하나로 한 줄을 쓰고, 그 숫자는
            아래 칸의 빈 자리에 들어갈 수 있다. 화면 낭독기는 `aria-label` 이 든다. */}
        <section className="review-panel review-transport" aria-label="棋譜">
          <div className="review-transport-row">
            <div className="review-buttons">
              <button type="button" onClick={() => goto(0)} disabled={ply === 0 && !branching} aria-label="開始局面">
                ⏮
              </button>
              <button
                type="button"
                onClick={() => goto(ply - 1)}
                disabled={ply === 0 && !branching}
                aria-label="一手戻る"
              >
                ◀
              </button>
              <button
                type="button"
                onClick={() => goto(ply + 1)}
                disabled={ply === last && !branching}
                aria-label="一手進む"
              >
                ▶
              </button>
              <button
                type="button"
                onClick={() => goto(last)}
                disabled={ply === last && !branching}
                aria-label="最終局面"
              >
                ⏭
              </button>
            </div>

            {/* **이 칸이 「지금 어디인가」이고, 그 칸이 곧 기보를 여는 버튼이다**
                (将棋ウォーズ의 그 바). 한때 「지금 어디」를 셋이 말했다 — 슬라이더 손잡이 ·
                `0 / 109 手` · 목록의 강조 줄. 숫자만으로는 **거기가 무슨 수였나**를 모르므로,
                남긴 하나는 手数가 아니라 **수의 이름**이다. */}
            <button
              type="button"
              className="review-jump"
              aria-expanded={kifuOpen}
              aria-controls="review-kifu-list"
              onClick={() => setKifuOpen((open) => !open)}
            >
              <span className="review-jump-label">{jumpLabel}</span>
              {/* 어디쯤인가. 이름(`101 ▲8五馬`)이 「무슨 수였나」를 말하고 이쪽이 「몇 번째인가」다 */}
              <span className="review-jump-count">
                {ply} / {last}
              </span>
              <span className="review-jump-caret" aria-hidden="true">
                {kifuOpen ? '▴' : '▾'}
              </span>
            </button>
          </div>

          {kifuOpen && (
            <ol id="review-kifu-list" className="review-kifu" ref={kifuRef}>
              <li className="review-kifu-start">
                <button
                  type="button"
                  className="review-kifu-row"
                  data-selected={(ply === 0 && !branching) || undefined}
                  onClick={() => jumpTo(0)}
                >
                  <span className="review-kifu-number">0</span>
                  <span className="review-kifu-move">開始局面</span>
                </button>
              </li>
              {rows.map((pair, i) => (
                <li key={pair[0]?.ply ?? pair[1]?.ply ?? i} className="review-kifu-pair">
                  {pair.map((move, column) =>
                    move ? (
                      <button
                        key={move.ply}
                        type="button"
                        className="review-kifu-row"
                        data-by={move.by}
                        data-selected={(ply === move.ply && !branching) || undefined}
                        onClick={() => jumpTo(move.ply)}
                      >
                        <span className="review-kifu-number">{move.ply}</span>
                        <span className="review-kifu-move">{move.ja || move.usi}</span>
                        {/* 이 手数에 물러진 수가 있었다. 확정된 수 옆에 서야 「이 수를 두기
                        전에 한 번 막혔다」로 읽힌다. */}
                        {stopped.has(move.ply) && (
                          <span className="review-kifu-mark" aria-label="介入あり">
                            介入
                          </span>
                        )}
                        {move.evalCp !== undefined && (
                          <span className="review-kifu-eval" data-sign={move.evalCp >= 0 ? 'plus' : 'minus'}>
                            {evalText(move.evalCp)}
                          </span>
                        )}
                      </button>
                    ) : (
                      // 빈 칸이 자리를 지킨다. 구멍이 난 기보에서도 열이 밀리지 않는다.
                      <span key={column} className="review-kifu-gap" aria-hidden="true" />
                    ),
                  )}
                </li>
              ))}
            </ol>
          )}
        </section>
      </div>

      <aside className="game-side">
        <div className="review-head">
          <button type="button" className="review-back" onClick={onBack}>
            ← 対局一覧
          </button>
          <p className="review-meta">
            <time dateTime={game.startedAt}>{dateJa(game.startedAt)}</time>
            <span className="review-result" data-result={game.result}>
              {resultJa(game.result)}
            </span>
          </p>
        </div>

        {promoting && (
          <div className="promotion" role="group" aria-label="成りの選択">
            <span>成りますか。</span>
            <button
              type="button"
              className="btn btn--primary"
              onClick={() => playBranch(toUsiMove(promoting.origin, promoting.to, true))}
            >
              成る
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => playBranch(toUsiMove(promoting.origin, promoting.to, false))}
            >
              不成
            </button>
          </div>
        )}

        <WhatIfPanel
          basePly={ply}
          node={shown}
          stale={!active}
          pending={pending}
          error={whatif.error}
          engineReady={engineReady}
          evalOf={whatif.evalOf}
          onBack={back}
          onRoot={toRoot}
        />

        {/* **한 목록이다.** 최선수·실제로 둔 수·물러진 수가 같은 국면의 같은 종류의 사실이라
            평가치 하나로 세운다 — 「내가 둔 것이 몇 번째쯤이었나」가 그 사이의 한 줄이 된다. */}
        <MoveOptions
          game={game}
          ply={ply}
          node={shown}
          measured={measured}
          chosen={chosen}
          // 실제로 둔 수를 누르는 것은 **가정이 아니라 진행**이다 — 같은 국면에 서면서
          // 화면만 「もしも」가 되는 것을 막는다(MoveOptions 의 `onPick`).
          onPick={(usi, played) => (played ? goto(ply + 1) : playBranch(usi))}
        />
      </aside>
    </div>
  );
}
