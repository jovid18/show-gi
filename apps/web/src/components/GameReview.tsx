import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Board, type LastMove, type Motion, type Ray } from './Board';
import { Hand } from './Hand';
import { WhatIfPanel } from './WhatIfPanel';
import { groupByOrigin, parseUsi, toUsiMove, type Destination } from '@/game/moves';
import { branchMotion, evalText, stepMotion } from '@/review/branch';
import { dateJa, resultJa } from '@/review/labels';
import type { GameDetail, ReviewIntervention } from '@/review/protocol';
import { useEngineReady } from '@/review/useReview';
import { useWhatIf } from '@/review/useWhatIf';
import { parseSfen, type Board as BoardModel } from '@/shogi/sfen';
import { fromUsi, toIndex } from '@/shogi/square';

/**
 * 한 판을 되짚는다.
 *
 * **판은 언제나 「지금 고른 手数까지 둔 뒤」다.** 그 국면은 서버가 준 SFEN 그대로이고
 * 화면은 수를 두지 않는다 — 대국에서 정한 것과 같은 자리다(화면은 규칙을 모른다).
 *
 * 개입을 고르면 판이 **그 수의 한 수 앞**으로 간다. 물러진 수는 기보에 없으므로 그 자리가
 * 유일하게 그것을 그릴 수 있는 국면이다 — 거기서 그 수를 화살표로 긋는다.
 *
 * **거기서 한 걸음 더 갈 수 있다.** 「그럼 어떻게 뒀어야 하나」와 「그 수를 뒀으면 어떻게
 * 됐나」를 판 위에서 직접 둬 본다(useWhatIf). 그 분기의 판도 서버가 준 것이다.
 */
interface GameReviewProps {
  game: GameDetail;
  onBack: () => void;
}

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

export function GameReview({ game, onBack }: GameReviewProps) {
  /** 지금 보고 있는 手数. 0이면 시작 국면이다. */
  const [ply, setPly] = useState(0);
  /** 고른 개입. 판이 그 수의 한 수 앞으로 가고 물러진 수가 그려진다. */
  const [focus, setFocus] = useState<number | null>(null);
  /** 지금 나는 움직임. `id`가 바뀔 때마다 그 칸에서 다시 난다. */
  const [motion, setMotion] = useState<Motion | null>(null);
  const motionId = useRef(0);
  /** 분기에서 고른 駒. 실제로 둔 판을 볼 때는 아무것도 안 고른다. */
  const [origin, setOrigin] = useState<string | null>(null);
  /** 成/不成 둘 다 되는 수. 물어보는 동안 다른 수를 못 두게 잡아 둔다. */
  const [promoting, setPromoting] = useState<{ origin: string; to: string } | null>(null);

  const { node: branch, pending, error, canBack, enter, play, back, close } = useWhatIf(game.id);
  const engineReady = useEngineReady();

  const last = game.moves.length;
  const focused = focus === null ? null : game.interventions[focus];

  /** 다음 움직임의 열쇠. 올려 두면 같은 칸에 두 번 들어와도 다시 난다(되잡기). */
  const nextMotionId = useCallback(() => ++motionId.current, []);

  const leaveBranch = useCallback(() => {
    close();
    setOrigin(null);
    setPromoting(null);
    setMotion(null);
  }, [close]);

  const goto = useCallback(
    (next: number) => {
      const target = Math.min(Math.max(next, 0), last);
      // 手数를 옮기면 회상도 분기도 끝난다. **둘 다 그 국면에서만 사실이다.**
      setFocus(null);
      leaveBranch();
      // **분기에서 나오는 길에는 움직임을 안 그린다.** 판이 다른 줄에서 통째로 갈아치워지는
      // 것이라, 그 위에서 駒 하나가 미끄러지면 「이 한 수로 이렇게 됐다」는 거짓말이 된다.
      setMotion(branch ? null : stepMotion(game.moves, ply, target, nextMotionId()));
      setPly(target);
    },
    [ply, last, game.moves, branch, leaveBranch, nextMotionId],
  );

  /** 개입을 고르면 그 수가 두어진 국면으로 간다 — **한 수 앞**이다. */
  const recall = useCallback(
    (index: number, at: number) => {
      leaveBranch();
      setMotion(null); // 뛰어간 자리다. 없던 한 수를 그리지 않는다
      setPly(Math.max(0, at - 1));
      setFocus((current) => (current === index ? null : index));
    },
    [leaveBranch],
  );

  /** 분기로 들어간다. 수를 주면 그 수부터 두고 시작한다 — 물러진 수가 그것이다. */
  const enterBranch = useCallback(
    (at: number, moves?: string[]) => {
      setOrigin(null);
      setPromoting(null);
      enter(at, moves);
    },
    [enter],
  );

  const playBranch = useCallback(
    (usi: string) => {
      setOrigin(null);
      setPromoting(null);
      play(usi);
    },
    [play],
  );

  // 분기가 한 걸음 나아가면 그 수가 판 위에서 움직인다. **판이 통째로 바뀌면 초심자는
  // 무엇이 변했는지 못 본다**(03-frontend.md §3) — 여기가 그 문장이 걸린 자리다.
  useEffect(() => {
    if (!branch) return;
    setMotion(branchMotion(branch, nextMotionId()));
  }, [branch, nextMotionId]);

  // ← → 로 한 수씩, Home·End 로 끝까지. 넘겨 보는 화면에서 손이 제일 먼저 가는 자리다.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // 슬라이더가 잡고 있을 때는 그쪽이 이미 같은 일을 한다. 두 번 움직이면 두 수씩 넘어간다.
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

  // 분기에 들어가 있으면 그 국면이고, 아니면 실제로 둔 판이다.
  const sfen = branch ? branch.sfen : ply === 0 ? game.startSfen : (game.moves[ply - 1]?.sfen ?? '');
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
    if (branch) {
      const played = branch.line.at(-1);
      return played ? squaresOf(played.usi) : null;
    }
    return focused || !current ? null : squaresOf(current.usi);
  }, [branch, focused, current]);

  /**
   * 물러진 수. 흰빛 두 칸과 화살표를 함께 쓴다.
   *
   * **打은 화살표가 안 나간다.** 판 위에 출발 칸이 없어서 駒台에서 재야 하는데, 리뷰는
   * 그 측정을 하지 않는다(대국 쪽 장치다). 그래도 두 칸 표식은 도착점을 짚으므로
   * 「어디에 놓으려 했나」는 남는다.
   *
   * **분기에 들어가면 안 그린다.** 그때 판은 그 수를 실제로 둔 뒤의 국면이라, 「두려던
   * 수」의 표식을 얹으면 이미 벌어진 일을 아직 안 벌어진 것처럼 말한다.
   */
  const recalling = focused !== null && !branch;
  const retracted = useMemo(
    () => (recalling && focused?.retractedUsi ? squaresOf(focused.retractedUsi) : null),
    [recalling, focused],
  );
  const ray = useMemo<Ray | null>(
    () => (retracted && retracted.from !== null ? { ...retracted, from: retracted.from, by: 'human' } : null),
    [retracted],
  );

  /**
   * 한 번이라도 막힌 手数. 기보 줄에 표식을 붙이는 데 쓴다.
   *
   * **몇 번인지는 안 센다.** 같은 국면에서 여러 번 물러지는 일이 실제로 있지만, 그 횟수는
   * 아래 개입 목록이 줄로 보여준다 — 기보 줄에 숫자까지 얹으면 「이 수가 몇 수째인가」와
   * 자리를 다툰다.
   */
  const stopped = useMemo(() => new Set(game.interventions.map((iv) => iv.ply)), [game.interventions]);

  const humanLabel = game.myColor === 'b' ? 'あなた' : '相手';
  const whiteLabel = game.myColor === 'b' ? '相手' : 'あなた';
  /** 사람의 駒台가 어느 쪽인가. 판은 안 뒤집고 라벨만 옮기므로(위) 여기서 갈린다. */
  const humanSide = game.myColor === 'b' ? 'black' : 'white';

  /**
   * 분기에서 지금 둘 수 있는 수.
   *
   * **목록에 있으면 둘 수 있고 없으면 못 둔다.** 二歩도 打ち歩詰め도 여기서 안 본다 —
   * 애초에 서버가 안 보낸다(game/moves.ts).
   */
  const grouped = useMemo(() => groupByOrigin(branch?.legalMoves ?? []), [branch?.legalMoves]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const playable = !!branch && branch.status === 'playing' && branch.yourTurn && !pending && !promoting;

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
   * **`scrollIntoView` 를 안 쓴다.** 그쪽은 「그 요소가 보이게」가 목적이라 목록이 아직
   * 넘치지 않으면 **페이지를** 스크롤하고, 좁은 화면에서는 그때 판이 시야 밖으로 밀린다
   * (棋譜 쪽에서 실제로 겪은 것과 같은 자리다). 목록의 scrollTop 만 움직이면 페이지는
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
  }, [ply]);

  return (
    <div className="game review">
      <div className="game-board">
        {/* **이 판은 실제로 벌어진 일이 아니다.** 옆 패널의 제목만으로는 판을 보는 동안
            그 사실이 안 남는다 — 되짚기와 같은 판·같은 駒台라 더 그렇다. */}
        {branch && (
          <p className="review-branch-badge" role="status">
            もしもの局面
          </p>
        )}

        <Hand
          side="white"
          label={whiteLabel}
          pieces={board?.hands.white ?? {}}
          selected={humanSide === 'white' && origin?.endsWith('*') ? origin : null}
          playable={
            humanSide === 'white' && playable ? new Set([...grouped.keys()].filter((o) => o.endsWith('*'))) : new Set()
          }
          onPick={humanSide === 'white' ? pickHand : () => {}}
        />

        {board ? (
          <Board
            board={board}
            lit={lit}
            selected={origin && !origin.endsWith('*') ? origin : null}
            lastMove={lastMove}
            checked={branch ? (branch.checked ?? null) : recalling ? null : (current?.checked ?? null)}
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
          selected={humanSide === 'black' && origin?.endsWith('*') ? origin : null}
          playable={
            humanSide === 'black' && playable ? new Set([...grouped.keys()].filter((o) => o.endsWith('*'))) : new Set()
          }
          onPick={humanSide === 'black' ? pickHand : () => {}}
        />
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

        <div className="review-controls">
          <div className="review-buttons">
            <button type="button" onClick={() => goto(0)} disabled={ply === 0 && !branch} aria-label="開始局面">
              ⏮
            </button>
            <button type="button" onClick={() => goto(ply - 1)} disabled={ply === 0 && !branch} aria-label="一手戻る">
              ◀
            </button>
            <button
              type="button"
              onClick={() => goto(ply + 1)}
              disabled={ply === last && !branch}
              aria-label="一手進む"
            >
              ▶
            </button>
            <button type="button" onClick={() => goto(last)} disabled={ply === last && !branch} aria-label="最終局面">
              ⏭
            </button>
          </div>

          {/* 132수를 한 수씩 눌러 가지 않게 한다. 긴 판에서 이것이 없으면 되짚기가 일이 된다. */}
          <input
            type="range"
            className="review-slider"
            min={0}
            max={last}
            value={ply}
            onChange={(e) => goto(Number(e.target.value))}
            aria-label="手数"
          />
          <p className="review-ply">
            {ply} / {last} 手
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

        {branch ? (
          <WhatIfPanel
            node={branch}
            pending={pending}
            error={error}
            canBack={canBack}
            onPlay={playBranch}
            onBack={back}
            onClose={leaveBranch}
          />
        ) : (
          <WhatIfEntry engineReady={engineReady} pending={pending} error={error} onEnter={() => enterBranch(ply)} />
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
                      // 물러진 수는 **고정이다.** 그 수를 두고 시작하는 것만 열어 둔다 —
                      // 여기서 아무 데나 갈 수 있으면 「무엇이 나빴나」가 흐려진다(§25).
                      canTry={engineReady !== false && !!iv.retractedUsi && iv.ply >= 1}
                      onTry={() => enterBranch(iv.ply - 1, iv.retractedUsi ? [iv.retractedUsi] : [])}
                    />
                  )}
                </li>
              ))}
            </ol>
          )}
        </section>

        <section className="review-panel" aria-label="棋譜">
          <h2 className="panel-title">棋譜</h2>
          <ol className="review-kifu" ref={kifuRef}>
            <li>
              <button
                type="button"
                className="review-kifu-row"
                data-selected={(ply === 0 && !branch) || undefined}
                onClick={() => goto(0)}
              >
                <span className="review-kifu-number">0</span>
                <span className="review-kifu-move">開始局面</span>
              </button>
            </li>
            {game.moves.map((move) => (
              <li key={move.ply}>
                <button
                  type="button"
                  className="review-kifu-row"
                  data-by={move.by}
                  data-selected={(ply === move.ply && !branch) || undefined}
                  onClick={() => goto(move.ply)}
                >
                  <span className="review-kifu-number">{move.ply}</span>
                  <span className="review-kifu-move">{move.ja || move.usi}</span>
                  {/* 이 手数에 물러진 수가 있었다. 확정된 수 옆에 서야 「이 수를 두기 전에
                      한 번 막혔다」로 읽힌다. */}
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
              </li>
            ))}
          </ol>
        </section>
      </aside>
    </div>
  );
}

/**
 * 분기로 들어가는 자리.
 *
 * **엔진이 없으면 버튼을 안 내놓는다.** 눌러도 503만 돌아오는 버튼은 「고장 났다」로
 * 읽히고, 실제로 고장 난 것은 이 화면이 아니다 — 되짚기는 그대로 돈다(server.go).
 */
function WhatIfEntry({
  engineReady,
  pending,
  error,
  onEnter,
}: {
  engineReady: boolean | null;
  pending: boolean;
  error: string | null;
  onEnter: () => void;
}) {
  return (
    <section className="review-panel review-whatif" aria-label="もしも">
      <h2 className="panel-title">もしも</h2>
      {engineReady === false ? (
        <p className="review-empty">エンジンが動いていないため、この局面から指し直すことはできません。</p>
      ) : (
        <>
          <p className="review-empty">この局面から指し直して、その先がどうなったかを見られます。</p>
          <button type="button" className="btn" disabled={pending} onClick={onEnter}>
            {pending ? '読んでいます…' : 'この局面から指してみる'}
          </button>
          {error && (
            <p className="rejection" role="alert">
              {error}
            </p>
          )}
        </>
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
 * 여기가 **↘ 방향**이다 — 「최선수」가 아니라 **두려고 했던 수**로 들어가는 입구이고,
 * 실제로 가르치는 것이 그쪽이다(06-status.md §25).
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
