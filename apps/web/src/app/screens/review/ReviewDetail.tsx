import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Board, type LastMove, type Ray } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { EvalGraph } from './EvalGraph';
import { WhatIfPanel } from './WhatIfPanel';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/libs/game/moves';
import { dateJa, resultJa } from '@/libs/review/labels';
import type { GameDetail, ReviewIntervention, ReviewMove } from '@/protocol/review';
import { useEngineReady } from '@/hooks/useReview';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import { fromUsi, toIndex, type Motion } from '@/models/square';
import { branchMotion, evalText, stepMotion } from '@/libs/whatif/branch';
import { httpSend } from '@/libs/whatif/http';
import { useWhatIf } from '@/hooks/useWhatIf';

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
  /** 고른 개입. 판이 그 수의 한 수 앞으로 가고 물러진 수가 그려진다. */
  const [focus, setFocus] = useState<number | null>(null);
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

  const last = game.moves.length;
  const focused = focus === null ? null : game.interventions[focus];

  /** 다음 움직임의 열쇠. 올려 두면 같은 칸에 두 번 들어와도 다시 난다(되잡기). */
  const nextMotionId = useCallback(() => ++motionId.current, []);

  const goto = useCallback(
    (next: number) => {
      const target = Math.min(Math.max(next, 0), last);
      // 手数를 옮기면 회상도 분기도 끝난다. **둘 다 그 국면에서만 사실이다.**
      setFocus(null);
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

  /** 개입을 고르면 그 수가 두어진 국면으로 간다 — **한 수 앞**이다. */
  const recall = useCallback(
    (index: number, ivPly: number) => {
      clear();
      setOrigin(null);
      setPromoting(null);
      setMotion(null); // 뛰어간 자리다. 없던 한 수를 그리지 않는다
      setPly(Math.max(0, ivPly - 1));
      setFocus((current) => (current === index ? null : index));
    },
    [clear],
  );

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
  useEffect(() => {
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
    return focused || !current ? null : squaresOf(current.usi);
  }, [branching, active, focused, current]);

  /**
   * 물러진 수. 흰빛 두 칸과 화살표를 함께 쓴다.
   *
   * **打은 화살표가 안 나간다.** 판 위에 출발 칸이 없어서 駒台에서 재야 하는데, 리뷰는
   * 그 측정을 하지 않는다(대국 쪽 장치다). 그래도 두 칸 표식은 도착점을 짚으므로
   * 「어디에 놓으려 했나」는 남는다.
   */
  const recalling = focused !== null && !branching;
  const retracted = useMemo(
    () => (recalling && focused?.retractedUsi ? squaresOf(focused.retractedUsi) : null),
    [recalling, focused],
  );

  /**
   * 판 위의 화살표. **회상에서는 물러진 수**, 그 밖에서는 **수번 쪽의 최선수**다.
   *
   * 둘은 동시에 뜨지 않는다 — 회상은 「네가 두려던 수」이고 이쪽은 「지금 최선은 무엇인가」라
   * 한 판에 겹치면 어느 쪽이 사실인지 알 수 없다.
   */
  const ray = useMemo<Ray | null>(() => {
    if (recalling) {
      return retracted && retracted.from !== null ? { ...retracted, from: retracted.from, by: 'human' } : null;
    }
    const best = active?.candidates[0];
    if (!best) return null;
    const squares = squaresOf(best.usi);
    if (!squares || squares.from === null) return null; // 打은 駒台를 재야 한다. 여기는 안 잰다
    return { from: squares.from, to: squares.to, by: active.yourTurn ? 'human' : 'engine' };
  }, [recalling, retracted, active]);

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
  const legal = recalling ? [] : (active?.legalMoves ?? []);
  const grouped = useMemo(() => groupByOrigin(legal), [legal]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const playable = !!active && !recalling && active.status === 'playing' && !pending && !promoting;
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
          <EvalGraph game={game} ply={ply} whatif={whatif} onPick={goto} />
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
          onPick={handSide === 'white' ? pickHand : () => {}}
        />

        {board ? (
          <Board
            board={board}
            lit={lit}
            selected={origin && !origin.endsWith('*') ? origin : null}
            lastMove={lastMove}
            checked={branching && active ? (active.checked ?? null) : recalling ? null : (current?.checked ?? null)}
            played={recalling ? retracted : null}
            replay={null}
            ray={ray}
            motion={motion}
            checks={[]}
            dimmed={recalling}
            dropFrom={null}
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
          onPick={handSide === 'black' ? pickHand : () => {}}
        />
        {/* **이동과 기보가 한 컨트롤이다**(将棋ウォーズ). 슬라이더는 뺐다 — 「지금 어디인가」를
            말하는 자리가 셋이었고, 그중 하나만 남긴 것이 아래 가운데 칸이다. */}
        <section className="review-panel review-transport" aria-label="棋譜">
          <h2 className="panel-title">棋譜 {last}手</h2>

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

        {active ? (
          <WhatIfPanel
            node={active}
            pending={pending}
            error={whatif.error}
            evalOf={whatif.evalOf}
            onPlay={playBranch}
            onBack={back}
            onRoot={toRoot}
          />
        ) : (
          <WhatIfHint engineReady={engineReady} pending={pending} error={whatif.error} />
        )}

        <section className="review-panel" aria-label="介入">
          <h2 className="panel-title">介入 {game.interventions.length}回</h2>

          {game.interventions.length === 0 ? (
            <p className="review-empty">この対局では一度も止まりませんでした。</p>
          ) : (
            <ol className="review-iv-list">
              {game.interventions.map((iv, i) => (
                <li key={`${iv.ply}-${i}`}>
                  <button
                    type="button"
                    className="review-iv"
                    data-active={focus === i || undefined}
                    onClick={() => recall(i, iv.ply)}
                  >
                    <span className="review-iv-ply">{iv.ply}手目</span>
                    <span className="review-iv-cat">{iv.categoryJa}</span>
                    <span className="review-iv-move">{iv.retractedJa || iv.retractedUsi}</span>
                    <span className="review-iv-delta">−{Math.round(iv.deltaWin * 100)}%</span>
                  </button>
                  {focus === i && (
                    <InterventionNote
                      intervention={iv}
                      canTry={engineReady !== false && !!iv.retractedUsi && iv.ply >= 1}
                      onTry={() => {
                        setFocus(null); // 판이 그 수를 실제로 둔 국면으로 간다. 회상은 끝난다
                        at(iv.ply - 1, iv.retractedUsi ? [iv.retractedUsi] : []);
                      }}
                    />
                  )}
                </li>
              ))}
            </ol>
          )}
        </section>
      </aside>
    </div>
  );
}

/**
 * 아직 국면을 못 물어본 자리.
 *
 * **엔진이 없으면 그 사실을 적는다.** 되짚기는 그대로 도는데(기록만 있으면 된다) 여기만
 * 안 되는 것이라, 아무 말도 없으면 화면이 고장 난 것으로 읽힌다.
 */
function WhatIfHint({
  engineReady,
  pending,
  error,
}: {
  engineReady: boolean | null;
  pending: boolean;
  error: string | null;
}) {
  return (
    <section className="review-panel review-whatif" aria-label="もしも">
      <h2 className="panel-title">もしも</h2>
      {engineReady === false ? (
        <p className="review-empty">エンジンが動いていないため、この局面から指し直すことはできません。</p>
      ) : (
        <p className="review-empty">{pending ? '読んでいます…' : 'この局面の駒を動かすと、そこから指し直せます。'}</p>
      )}
      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

/**
 * 왜 나빴는가. **문구는 서버가 만든 것을 그대로 그린다.**
 *
 * 대국 중에 나갔던 문장은 어디에도 저장하지 않으므로(카테고리만 남는다) 이것은 그때
 * 그 문장과 글자까지 같지는 않다. 같은 사실에서 나온 결정적 문구다.
 *
 * 「この手を指してみる」가 **물러진 수로 들어가는 입구**다 — 최선수가 아니라 두려고 했던
 * 수이고, 실제로 가르치는 것이 그쪽이다(06-status.md §25).
 */
function InterventionNote({
  intervention,
  canTry,
  onTry,
}: {
  intervention: ReviewIntervention;
  canTry: boolean;
  onTry: () => void;
}) {
  return (
    <div className="review-iv-note">
      {intervention.message && <p>{intervention.message}</p>}
      {intervention.retractedJa && (
        <p className="review-iv-hint">
          この局面で <strong>{intervention.retractedJa}</strong> を指して戻されました。
        </p>
      )}
      {canTry && (
        <button type="button" className="btn" onClick={onTry}>
          この手を指してみる
        </button>
      )}
    </div>
  );
}
