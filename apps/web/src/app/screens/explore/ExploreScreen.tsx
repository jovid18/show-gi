import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { ReactElement } from 'react';

import { Candidates } from './Candidates';
import { Snapshots } from './Snapshots';
import { Board, type Ray } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { useEngineReady } from '@/hooks/useReview';
import { useMoveSound } from '@/hooks/useMoveSound';
import { useWhatIf } from '@/hooks/useWhatIf';
import { groupByOrigin, squaresOf, toUsiMove, type Destination } from '@/libs/game/moves';
import { getPlaying, subscribePlaying } from '@/libs/game/playing';
import { exploreSend } from '@/libs/explore/http';
import { baselineNoteJa, exploreStatusJa, sideJa } from '@/libs/explore/text';
import { branchMotion, scoreJa } from '@/libs/whatif/branch';
import type { Side } from '@/models/piece';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import type { Motion } from '@/models/square';
import type { ExploreNode } from '@/protocol/explore';
import { fetchHandicaps, type Handicap } from '@/protocol/handicaps';
import { navigate } from '@/routes/router';

/**
 * 「検討」 — 手合割을 골라 0手目부터 직접 판을 움직여 보면서 형세와 최선수 셋을 읽는다.
 *
 * 되짚기·개입 카드와 같은 장치를 쓴다(`useWhatIf`). 갈리는 것은 뿌리 하나뿐이고
 * (journal §37 · §85) 여기의 뿌리는 手合割 표다 — 그래서 이 화면은 판을 서버에 안 보낸다.
 * 보내는 것은 手合割 id와 수순이고, 서버가 매번 되짚어 한 수씩 룰 엔진에 검증시킨다.
 *
 * 줄의 정본은 주소다. 화면이 상태로 들고 있지 않고 `?m=` 에 적는다 — 새로고침·뒤로
 * 가기·링크 공유가 그것으로 살아난다. 한 수 두는 것은 「주소를 고쳐 쓰는 일」이고, 그러면
 * 줄을 들고 있는 자리가 하나뿐이라 판과 주소가 어긋날 수 없다.
 *
 * 대국 중에는 열지 않는다. 최선수 셋을 아무 국면에서나 답하는 화면이라, 두는 중에
 * 열리면 「평소엔 최선수를 보여주지 않는다」가 탭 하나로 뚫린다(01-core.md §1 · §7).
 * 헤더도 그때 이 탭을 안 그린다(App.tsx) — 벽이 둘이다.
 */
interface ExploreScreenProps {
  /** 手合割 id. 빈 값이 平手다. 주소에서 온다. */
  handicap: string;
  /** 지금까지 둔 수순. 주소에서 온다 — 이 화면이 들고 있는 것이 아니다. */
  moves: string[];
}

export function ExploreScreen({ handicap, moves }: ExploreScreenProps) {
  const playing = useSyncExternalStore(subscribePlaying, getPlaying);
  const engineReady = useEngineReady();

  const [handicaps, setHandicaps] = useState<Handicap[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    void fetchHandicaps(controller.signal).then(setHandicaps);
    return () => controller.abort();
  }, []);

  /**
   * 줄의 열쇠. 배열이 아니라 문자열로 의존성에 넣는다 — `moves` 는 주소를 읽을 때마다
   * 새 배열이라(routes/router.ts) 그대로 걸면 매 렌더마다 같은 자리를 다시 묻는다.
   */
  const line = moves.join(',');
  const urlMoves = useMemo(() => (line === '' ? [] : line.split(',')), [line]);

  const send = useMemo(() => exploreSend(handicap), [handicap]);
  // `resetKey` 가 手合割이다. 열쇠는 줄만 보므로(`useWhatIf` 의 `keyOf`) 안 비우면
  // 六枚落ち의 0手目가 平手의 0手目와 같은 자리로 읽힌다.
  const whatif = useWhatIf<ExploreNode>(send, handicap);
  const { node, pending, error, at } = whatif;

  /**
   * 주소가 바뀌면 그 국면을 묻는다. 이 효과가 이 화면의 유일한 흐름이다 — 누르는 쪽은
   * 주소만 고치고, 판이 서는 것은 여기서 시작된다.
   *
   * 서버가 이미 잰 국면이면 왕복도 탐색도 없고(`positions`), 지나온 자리면 왕복조차
   * 없다(`useWhatIf` 의 `seen`).
   */
  useEffect(() => {
    if (playing || engineReady === false) return;
    at(0, urlMoves);
  }, [playing, engineReady, handicap, urlMoves, at]);

  /**
   * 판에 얹을 수 있는 노드인가.
   *
   * 다른 줄의 노드로 두게 하지 않는다. 답을 기다리는 동안 판은 직전 국면을 그리고
   * 있으므로, 그 국면의 합법수로 지금 줄에 수를 더하면 서버가 거절한다.
   */
  const active = node && node.line.length === urlMoves.length ? node : null;
  /** 그리고 있는 것. 기다리는 동안 직전 것을 그대로 둔다 — 흐리게만 한다(`stale`). */
  const shown = node;
  const stale = !active;

  const [origin, setOrigin] = useState<string | null>(null);
  const [promoting, setPromoting] = useState<{ origin: string; to: string } | null>(null);
  const [flipped, setFlipped] = useState(false);
  const [motion, setMotion] = useState<Motion | null>(null);
  const motionId = useRef(0);
  const [soundOn, toggleSound] = useMoveSound(shown?.ply ?? 0);

  /** 주소를 고쳐 쓴다. 한 수 두는 것이 이 일이다. */
  const go = useCallback(
    (next: string[]) => {
      setOrigin(null);
      setPromoting(null);
      // 이력을 쌓지 않는다. 주소는 공유와 새로고침을 위해 따라와야 하지만, 40手를
      // 걸어 본 사람이 화면을 벗어나려고 뒤로 가기를 40번 누르게 두지 않는다.
      navigate({ name: 'explore', handicap, moves: next }, { replace: true });
    },
    [handicap],
  );

  const play = useCallback((usi: string) => go([...urlMoves, usi]), [go, urlMoves]);
  const back = useCallback(() => go(urlMoves.slice(0, -1)), [go, urlMoves]);
  const toStart = useCallback(() => go([]), [go]);

  /**
   * 같은 자리를 다시 묻는다. 없으면 첫 요청이 실패한 자리가 막다른 길이다.
   *
   * 이 표면의 실패 둘은 설계된 것이다 — 검토가 이미 하나 돌고 있으면 429이고
   * (`exploreSlots`), 엔진이 못 답하면 503이다. 그런데 0手目에서 그러면 판이 안 서고
   * 되돌릴 줄도 없어서(`branching` 이 false다) 누를 것이 하나도 남지 않는다. 주소가 같으니
   * 手合割을 다시 눌러도 `navigate` 가 같은 자리로 보고 아무것도 안 한다.
   */
  const reload = useCallback(() => at(0, urlMoves), [at, urlMoves]);

  /**
   * 다른 줄을 연다 — 手合割을 고르는 것과 저장한 국면을 불러오는 것 둘이다. 이쪽은
   * 이력을 쌓는다(`replace` 없이).
   *
   * 지금 걸어 보던 줄이 통째로 없어지는데, 이력에 남겨 두면 뒤로 가기 한 번으로 방금
   * 보던 줄이 그대로 돌아온다 — 「정말 버립니까」를 묻지 않는 이유다.
   */
  const openLine = useCallback((id: string, next: string[]) => {
    navigate({ name: 'explore', handicap: id, moves: next });
  }, []);

  const pickHandicap = useCallback((id: string) => openLine(id, []), [openLine]);

  const board = useMemo<BoardModel | null>(() => {
    if (!shown?.sfen) return null;
    try {
      return parseSfen(shown.sfen);
    } catch {
      return null; // 못 읽는 국면으로 판을 그리느니 그 자리를 비운다
    }
  }, [shown?.sfen]);

  const legal = active?.legalMoves ?? [];
  const grouped = useMemo(() => groupByOrigin(legal), [legal]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const playable = !!active && active.status === 'playing' && !pending && !promoting;
  /** 지금 수번의 駒台만 집을 수 있다. 판을 뒤집어도 이 값은 안 바뀐다. */
  const handSide: Side = shown?.turn === 'w' ? 'white' : 'black';
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
      play(toUsiMove(origin, usi, dest.promote));
      return;
    }
    setOrigin(grouped.has(usi) && usi !== origin ? usi : null);
  };

  /** 이 판을 만든 수. 출발 칸과 도착 칸을 함께 짚는다. */
  const lastMove = useMemo(() => {
    const played = shown?.line.at(-1);
    return played ? squaresOf(played.usi) : null;
  }, [shown]);

  /**
   * 판 위의 초록 화살표 — 수번 쪽의 최선수다.
   *
   * 되짚기와 갈리는 자리다. 저쪽은 확정된 판 위에 안 긋는다: 넘겨 보는 것만으로 답이
   * 그려지면 스스로 찾을 자리가 없어지기 때문이다(ReviewDetail 의 `ray`). 검토는 답을
   * 보러 오는 화면이라 그 근거가 서지 않는다 — 옆의 목록이 이미 같은 수를 첫 줄로 들고
   * 있고, 판에 안 그으면 그 수가 어디서 어디로 가는지를 좌표로 읽어야 한다.
   */
  const ray = useMemo<Ray | null>(() => {
    const best = active?.candidates[0];
    if (!best) return null;
    const squares = squaresOf(best.usi);
    if (!squares) return null;
    // 打이면 도착점만 찍힌다. 판 위에 출발 칸이 없어서 駒台에서 자리를 재야 하는데
    // (Board 의 `dropFrom`), 그 측정 코드가 대국·되짚기에 이미 두 벌이라 세 벌로 늘리지
    // 않았다. 어느 駒인지는 목록의 첫 줄이 말한다.
    return { from: squares.from, to: squares.to, by: active.yourTurn ? 'human' : 'engine' };
  }, [active]);

  /**
   * 한 수가 판 위에서 움직인다. 판이 통째로 바뀌면 초심자는 무엇이 변했는지 못 본다
   * (03-frontend.md §3).
   *
   * 줄이 자랐을 때만 그린다. 물리는 것은 판을 새로 받는 일이라(`branchMotion`) 거기에
   * 움직임을 얹으면 방금 지운 수가 한 번 더 놓이는 것처럼 보인다.
   *
   * `useLayoutEffect` 여야 한다. 페인트 뒤에 붙이면 駒가 도착 칸에 한 번 뜬 다음
   * 출발 칸으로 되돌아가 다시 와서, 한 수에 駒가 두 번 움직인다(되짚기에서 물린 자리다).
   */
  const prevLine = useRef(0);
  useLayoutEffect(() => {
    const length = node?.line.length ?? 0;
    const grew = length === prevLine.current + 1;
    prevLine.current = length;
    setMotion(node && grew ? branchMotion(node, ++motionId.current) : null);
  }, [node]);

  // ← 로 한 수 물리고, Home 으로 처음으로. 넘겨 보는 화면에서 손이 제일 먼저 가는 자리다.
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === 'ArrowLeft') back();
      else if (e.key === 'Home') toStart();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [back, toStart]);

  /**
   * 駒台 하나. 부르는 쪽이 색이 아니라 자리를 정한다 — 판이 뒤집히면 持ち駒도 따라와야
   * 하는데, 색으로 박아 두면 판만 돌고 駒台가 그대로 남는다(되짚기와 같은 자리).
   */
  const hand = (side: Side): ReactElement => (
    <Hand
      side={side}
      label={sideJa(side === 'black' ? 'b' : 'w', !!shown?.handicapJa)}
      pieces={board?.hands[side] ?? {}}
      selected={handSide === side && origin?.endsWith('*') ? origin : null}
      playable={handSide === side ? droppable : new Set()}
      onPick={handSide === side && playable ? (next) => setOrigin(next === origin ? null : next) : () => {}}
    />
  );

  if (playing) {
    // 두는 중에는 안 연다(위 컴포넌트 주석). 헤더가 이 탭을 안 그리므로 여기 오는
    // 길은 링크와 새로고침뿐이고, 그때 판을 그려 놓고 잠그면 고장으로 읽힌다.
    return (
      <div className="explore-closed">
        <h1 className="explore-title">検討</h1>
        <p className="review-status">対局中は検討できません。対局を終えると、ここで自由に並べられます。</p>
      </div>
    );
  }

  const score = shown ? scoreJa(shown.evalCp, shown.mateIn) : '';
  const baseline = baselineNoteJa(shown);
  const branching = urlMoves.length > 0;

  return (
    <div className="explore">
      <div className="game" data-flipped={flipped || undefined}>
        <div className="game-board">
          {/* 手合割. 목록에 平手가 없다 — 접지 않는 것이 기본값이라 이 자리가 그 버튼을
              직접 그린다(protocol/handicaps.ts). */}
          <div className="explore-handicaps" role="group" aria-label="手合割">
            <button
              type="button"
              className="explore-handicap"
              data-on={handicap === '' || undefined}
              aria-pressed={handicap === ''}
              onClick={() => pickHandicap('')}
            >
              平手
            </button>
            {handicaps.map((h) => (
              <button
                key={h.id}
                type="button"
                className="explore-handicap"
                data-on={handicap === h.id || undefined}
                aria-pressed={handicap === h.id}
                title={h.note}
                onClick={() => pickHandicap(h.id)}
              >
                {h.name}
              </button>
            ))}
          </div>

          {/* 판이 없으면 駒台도 안 그린다. 빈 받침 둘만 남으면 판이 있어야 할 자리에
              구멍이 뚫린 그림이 되고, 그건 고장으로 읽힌다 — 거절된 링크로 들어오면 실제로
              그 그림이었다. 받침이 자리를 지키는 규칙은 판이 있을 때의 것이다(`Hand`). */}
          {board ? (
            <>
              {hand(flipped ? 'black' : 'white')}

              <Board
                board={board}
                lit={lit}
                selected={origin && !origin.endsWith('*') ? origin : null}
                lastMove={lastMove}
                checked={shown?.checked ?? null}
                played={null}
                replay={null}
                ray={ray}
                motion={motion}
                checks={[]}
                // 탈색하지 않는다. 탈색은 「지금이 아니다」를 말하는 장치인데, 이 화면은
                // 전부가 「지금이 아니다」다 — 되짚기와 같은 판단이다.
                dimmed={false}
                dropFrom={null}
                hintSquare={null}
                hintRay={null}
                mateHeat={0}
                // 그늘(`相手の利き`)의 기준. 아래에 있는 쪽이다 — 검토에는 「나」가 없어서
                // 판을 돌리면 보는 쪽도 함께 돈다.
                me={flipped ? 'white' : 'black'}
                flipped={flipped}
                interactive={playable}
                sound={{ on: soundOn, toggle: toggleSound }}
                flip={{ on: flipped, toggle: () => setFlipped((f) => !f) }}
                onSquare={onSquare}
              />

              {hand(flipped ? 'white' : 'black')}
            </>
          ) : shown ? (
            <p className="review-broken">この局面は表示できません。</p>
          ) : (
            // 아직 국면이 없다. 「표시할 수 없다」로 적으면 안 된다 — 링크의 수순이
            // 거절된 자리에서도 그 문장이 뜨고, 그때 못 그리는 것은 판이 아니라 그 줄이다.
            // 엔진이 없으면 기다릴 것도 없어서 「읽는 중」이 영원히 오지 않는 약속이 된다.
            // 두 이유 다 옆 패널이 이미 말한다(`error`).
            <p className="review-status">{error || engineReady === false ? '' : '局面を読み込んでいます…'}</p>
          )}

          {/* 되돌리는 둘. 줄이 없으면 이 줄 자체가 안 선다 — 0手目에서 물릴 것이 없는데
              버튼이 서 있으면 그건 눌러도 안 되는 버튼이고, 그렇게 두면 다음에 진짜로 못
              누를 때 같이 무시된다(홈 메뉴가 쓰는 규칙과 같다). `.btn` 에는 disabled 모양이
              따로 없어서 더 그렇다.

              「盤を反転」은 여기 없다. 판이 주는 손잡이 줄로 옮겼다(`Board` 의 `flip`) —
              착수음·그늘과 같이 「판을 어떻게 보나」라 셋이 한자리에 있어야 한다. */}
          {branching && (
            <div className="explore-controls">
              <button type="button" className="btn" disabled={pending} onClick={back}>
                一手戻る
              </button>
              <button type="button" className="btn" disabled={pending} onClick={toStart}>
                最初へ
              </button>
            </div>
          )}
        </div>

        <aside className="game-side">
          <section className="review-panel explore-head" aria-label="形勢">
            <div className="explore-head-top">
              <h1 className="explore-title">検討</h1>
              {/* 값은 끝난 국면에는 없다. 0으로 채우면 호각과 구별이 안 된다. */}
              {score && (
                <span className="explore-score" data-stale={stale || undefined}>
                  {score}
                </span>
              )}
            </div>

            <p className="explore-status" data-stale={stale || undefined}>
              {engineReady === false
                ? 'エンジンが動いていないため、いまは検討できません。'
                : exploreStatusJa(shown, pending)}
            </p>

            {/* 「이 手合의 互角은 얼마인가」. 평가치를 옮기는 대신 기준선을 말한다 —
                숫자의 자를 되짚기 그래프와 같게 두려면 값을 옮길 수가 없다(journal §84). */}
            {baseline && <p className="explore-baseline">{baseline}</p>}

            {error && (
              <div className="explore-error" role="alert">
                <p className="rejection">{error}</p>
                {/* 다시 누를 수 있어야 한다. 429·503은 설계된 실패라(위 `reload`)
                    한 번 더 물으면 그냥 되는 경우가 대부분이다. */}
                <button type="button" className="btn" disabled={pending} onClick={reload}>
                  もう一度読み込む
                </button>
              </div>
            )}
          </section>

          {promoting && (
            <div className="promotion" role="group" aria-label="成りの選択">
              <span>成りますか。</span>
              <button
                type="button"
                className="btn btn--primary"
                onClick={() => play(toUsiMove(promoting.origin, promoting.to, true))}
              >
                成る
              </button>
              <button
                type="button"
                className="btn"
                onClick={() => play(toUsiMove(promoting.origin, promoting.to, false))}
              >
                不成
              </button>
            </div>
          )}

          {/* `active` 가 아니라 `shown` 을 넘긴다. `active` 는 「이 줄의 노드인가」라
              판을 잠그는 데 쓰는 값이고, 이 목록에 넘기면 한 수 둘 때마다 세 줄이 사라졌다가
              다시 선다 — `Candidates` 가 막겠다고 적어 둔 그 그림이다. 자리는 지키고
              흐리게 하고 못 누르게 한다(`stale`). */}
          <Candidates node={shown} stale={stale} onPick={play} />

          {/* 지금까지의 줄. 실제 기보와 같은 어휘로 같은 모양으로 선다 — 手数 · 수 · cp.
              값은 지나온 자리에서 꺼낸다(`evalOf`) — 다시 묻지 않으므로 추가 탐색이 0이다. */}
          {shown && shown.line.length > 0 && (
            <section className="review-panel explore-line-panel" aria-label="並べた手順">
              <h2 className="panel-title">ならべた手順</h2>
              <ol className="review-whatif-line" data-stale={stale || undefined}>
                {shown.line.map((move, i) => {
                  const scored = whatif.evalOf(i + 1);
                  return (
                    <li key={move.ply} data-by={move.by}>
                      <span className="review-kifu-number">{move.ply}</span>
                      <span className="review-kifu-move">{move.ja || move.usi}</span>
                      <span className="review-kifu-eval">{scored ? scoreJa(scored.cp, scored.mateIn) : ''}</span>
                    </li>
                  );
                })}
              </ol>
            </section>
          )}

          {/* 저장한 국면. 목록의 마지막이다 — 판을 보며 읽는 것(형세·최선수·수순)이 위에
              서고, 여기는 「다른 자리로 옮겨 간다」라 축이 다르다. 줄 수가 사람마다 다르고
              상한이 없으므로 위에 두면 그 아래가 화면 밖으로 밀린다. */}
          <Snapshots handicap={handicap} moves={urlMoves} savable={!!active} onLoad={openLine} />
        </aside>
      </div>
    </div>
  );
}
