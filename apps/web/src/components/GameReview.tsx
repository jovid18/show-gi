import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Board, type LastMove, type Ray } from './Board';
import { Hand } from './Hand';
import { parseUsi } from '@/game/moves';
import { dateJa, resultJa } from '@/review/labels';
import type { GameDetail, ReviewIntervention } from '@/review/protocol';
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

/** 평가치를 부호까지 읽히게 적는다. **플레이어 관점**이다. */
function evalText(cp: number): string {
  return cp > 0 ? `+${cp}` : `${cp}`;
}

export function GameReview({ game, onBack }: GameReviewProps) {
  /** 지금 보고 있는 手数. 0이면 시작 국면이다. */
  const [ply, setPly] = useState(0);
  /** 고른 개입. 판이 그 수의 한 수 앞으로 가고 물러진 수가 그려진다. */
  const [focus, setFocus] = useState<number | null>(null);

  const last = game.moves.length;
  const focused = focus === null ? null : game.interventions[focus];

  const goto = useCallback((next: number) => {
    setPly(next);
    // 手数를 옮기면 회상은 끝난다. 물러진 수는 그 국면에서만 사실이다.
    setFocus(null);
  }, []);

  /** 개입을 고르면 그 수가 두어진 국면으로 간다 — **한 수 앞**이다. */
  const recall = useCallback((index: number, at: number) => {
    setPly(Math.max(0, at - 1));
    setFocus((current) => (current === index ? null : index));
  }, []);

  // ← → 로 한 수씩, Home·End 로 끝까지. 넘겨 보는 화면에서 손이 제일 먼저 가는 자리다.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // 슬라이더가 잡고 있을 때는 그쪽이 이미 같은 일을 한다. 두 번 움직이면 두 수씩 넘어간다.
      if (e.target instanceof HTMLInputElement) return;
      switch (e.key) {
        case 'ArrowLeft':
          goto(Math.max(0, ply - 1));
          break;
        case 'ArrowRight':
          goto(Math.min(last, ply + 1));
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

  const sfen = ply === 0 ? game.startSfen : (game.moves[ply - 1]?.sfen ?? '');
  const board = useMemo<BoardModel | null>(() => {
    if (!sfen) return null;
    try {
      return parseSfen(sfen);
    } catch {
      return null; // 못 읽는 국면으로 판을 그리느니 그 자리를 비운다
    }
  }, [sfen]);

  const current = ply === 0 ? null : (game.moves[ply - 1] ?? null);

  /** 이 판을 만든 수. 회상 중에는 안 짚는다 — 그때 주인공은 물러진 수다. */
  const lastMove = useMemo(() => (focused || !current ? null : squaresOf(current.usi)), [focused, current]);

  /**
   * 물러진 수. 흰빛 두 칸과 화살표를 함께 쓴다.
   *
   * **打은 화살표가 안 나간다.** 판 위에 출발 칸이 없어서 駒台에서 재야 하는데, 리뷰는
   * 그 측정을 하지 않는다(대국 쪽 장치다). 그래도 두 칸 표식은 도착점을 짚으므로
   * 「어디에 놓으려 했나」는 남는다.
   */
  const retracted = useMemo(() => (focused?.retractedUsi ? squaresOf(focused.retractedUsi) : null), [focused]);
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
        <Hand
          side="white"
          label={whiteLabel}
          pieces={board?.hands.white ?? {}}
          selected={null}
          playable={new Set()}
          onPick={() => {}}
        />

        {board ? (
          <Board
            board={board}
            lit={new Set()}
            selected={null}
            lastMove={lastMove}
            checked={focused ? null : (current?.checked ?? null)}
            played={focused ? retracted : null}
            replay={null}
            ray={ray}
            checks={[]}
            dimmed={focused !== null}
            dropFrom={null}
            hintSquare={null}
            hintRay={null}
            mateHeat={0}
            interactive={false}
            onSquare={() => {}}
          />
        ) : (
          <p className="review-broken">この手からは局面を再現できません。</p>
        )}

        <Hand
          side="black"
          label={humanLabel}
          pieces={board?.hands.black ?? {}}
          selected={null}
          playable={new Set()}
          onPick={() => {}}
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
            <button type="button" onClick={() => goto(0)} disabled={ply === 0} aria-label="開始局面">
              ⏮
            </button>
            <button type="button" onClick={() => goto(ply - 1)} disabled={ply === 0} aria-label="一手戻る">
              ◀
            </button>
            <button type="button" onClick={() => goto(ply + 1)} disabled={ply === last} aria-label="一手進む">
              ▶
            </button>
            <button type="button" onClick={() => goto(last)} disabled={ply === last} aria-label="最終局面">
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
                  {focus === i && <InterventionNote intervention={iv} />}
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
                data-selected={ply === 0 || undefined}
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
                  data-selected={ply === move.ply || undefined}
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
 * 왜 나빴는가. **문구는 서버가 만든 것을 그대로 그린다.**
 *
 * 대국 중에 나갔던 문장은 어디에도 저장하지 않으므로(카테고리만 남는다) 이것은 그때
 * 그 문장과 글자까지 같지는 않다. 같은 사실에서 나온 결정적 문구다.
 */
function InterventionNote({ intervention }: { intervention: ReviewIntervention }) {
  return (
    <div className="review-iv-note">
      {intervention.message && <p>{intervention.message}</p>}
      {intervention.retractedJa && (
        <p className="review-iv-hint">
          この局面で <strong>{intervention.retractedJa}</strong> を指して戻されました。
        </p>
      )}
    </div>
  );
}
