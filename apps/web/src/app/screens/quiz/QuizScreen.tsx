import { useMemo, useRef, useState, type ReactNode } from 'react';

import { Board } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { groupByOrigin, squaresOf, toUsiMove, type Destination, type MoveSquares } from '@/libs/game/moves';
import { parseSfen, type Board as BoardModel } from '@/models/sfen';
import type { Side } from '@/models/piece';
import { navigate } from '@/routes/router';
import type { BestItem, MateItem, MateResult, QuizPayload } from '@/protocol/quiz';
import { useBestGrader, useMateGrader, useQuiz } from '@/hooks/useQuiz';

/**
 * 그 판에서 뽑은 문항을 푸는 화면.
 *
 * **문제도 정답도 이 판에서 나왔다.** 문제집을 들여오지 않은 것이 이 기능의 요점이다 —
 * 자기 기보에서 만들면 저작권도 없고, 「내가 놓친 詰み」이라는 말이 성립한다.
 *
 * **채점은 서버가 한다.** 정답이 화면에 오는 것은 답을 낸 뒤이고, 그래서 판을 열어 보는
 * 것으로는 답을 알 수 없다.
 */
export function QuizScreen({ id }: { id: number }) {
  const { loaded, reload, gaveUp } = useQuiz(id);
  const back = (): void => navigate({ name: 'review', id });

  if (loaded.state === 'loading') {
    return <p className="review-status">読み込み中…</p>;
  }
  if (loaded.state === 'error') {
    return (
      <p className="review-status" role="alert">
        {loaded.message}
        <button type="button" className="review-retry" onClick={reload}>
          もう一度
        </button>
        <button type="button" className="review-retry" onClick={back}>
          この対局へ
        </button>
      </p>
    );
  }
  return <Questions id={id} quiz={loaded.data} gaveUp={gaveUp} reload={reload} onBack={back} />;
}

function Questions({
  id,
  quiz,
  gaveUp,
  reload,
  onBack,
}: {
  id: number;
  quiz: QuizPayload;
  gaveUp: boolean;
  reload: () => void;
  onBack: () => void;
}) {
  const best = quiz.best ?? [];
  const empty = !quiz.mate && best.length === 0;

  /** 지금 보고 있는 문항. 0부터. */
  const [at, setAt] = useState(0);

  /**
   * 문항 하나가 한 페이지다. **詰み이 먼저다** — 그 판에서 가장 큰 사건이고, 「최선수는?」은
   * 그 판의 어느 국면이라도 될 수 있다.
   */
  const pages = useMemo(() => {
    const out: { key: string; node: ReactNode }[] = [];
    if (quiz.mate) out.push({ key: 'mate', node: <MateQuestion id={id} item={quiz.mate} /> });
    for (const item of best) {
      out.push({ key: `best-${item.index}`, node: <BestQuestion id={id} item={item} /> });
    }
    return out;
  }, [id, quiz.mate, best]);

  return (
    <section className="quiz" aria-label="この対局から出た問題">
      <div className="quiz-head">
        <h2 className="panel-title">この対局から出た問題</h2>
        <button type="button" className="review-retry" onClick={onBack}>
          この対局へ
        </button>
      </div>

      {/* **「아직 만드는 중」과 「문항이 없다」를 갈라 말한다.** 만드는 데 수십 초가 걸려서,
          그 사이에 「問題はありません」을 그리면 그것이 거짓이 된다. */}
      {/* **기다리기를 그만둔 것과 문항이 없는 것은 다른 말이다.** 그만둔 쪽에서 아는 것은
          「정해진 동안 안 왔다」뿐이라, 없다고 단정하면 그것이 거짓이 된다. */}
      {!quiz.ready && gaveUp ? (
        <p className="quiz-status">
          問題がまだ届きません。時間をおいてから、もう一度開いてみてください。
          <button type="button" className="review-retry" onClick={reload}>
            もう一度
          </button>
        </p>
      ) : !quiz.ready ? (
        <p className="quiz-status" role="status">
          この対局から問題を作っています。しばらくお待ちください。
        </p>
      ) : empty ? (
        // **이유를 말하지 않는다.** 「詰み이 없었고 뚜렷한 한 수도 없었다」는 서버가 모르는
        // 사실이다 — 생성기가 없는 배포는 아무것도 안 찾아보고 빈 행을 남기고(ws.go), 트리가
        // 깨져 詰み 문항이 빠진 판도 여기로 온다(quiz.go get). 초심자가 확인할 수 없는 것을
        // 말하지 않는 것이 이 제품의 규칙이고, 그 규칙이 가장 걸리는 자리가 빈 화면이다.
        <p className="quiz-status">この対局からは問題が作れませんでした。</p>
      ) : null}

      {pages.length > 0 && (
        <>
          {/* **한 번에 한 문항이다.** 넷을 한 페이지에 늘어놓으면 판 넷이 세로로 쌓여
              스크롤이 생기고, 지금 푸는 문항이 화면 밖으로 나간다(§6 #9).

              **넘어간 문항을 지우지 않는다** — `hidden` 으로 덮어 둔다. 언마운트하면 채점
              결과와 **시도 횟수**가 함께 사라져서, 돌아왔을 때 세 번 틀린 문항이 처음
              열린 것처럼 된다. `hidden` 은 접근성 트리에서도 빠지므로 낭독기가 안 읽는다. */}
          {pages.map((page, i) => (
            <div key={page.key} hidden={i !== at}>
              {page.node}
            </div>
          ))}

          <nav className="quiz-nav" aria-label="問題の切り替え">
            <button type="button" className="review-retry" onClick={() => setAt(at - 1)} disabled={at === 0}>
              ← 前の問題
            </button>
            {/* 「어디쯤인가」는 한 자리에서만 말한다 — 되짚기의 이동 바와 같은 판단이다. */}
            <span className="quiz-nav-count">
              {at + 1} / {pages.length}
            </span>
            <button
              type="button"
              className="review-retry"
              onClick={() => setAt(at + 1)}
              disabled={at === pages.length - 1}
            >
              次の問題 →
            </button>
          </nav>
        </>
      )}
    </section>
  );
}

/**
 * 詰み 문항.
 *
 * **王手만 빛난다**(`legalMoves` 가 王手만 담아 온다). 詰将棋에서 攻方은 매 수 王手를 걸어야
 * 하고, 그 규약을 화면이 판으로 가르치는 자리다 — 글로 적어 두기만 하면 초심자는 그것을
 * 모른 채 오답을 받는다.
 */
function MateQuestion({ id, item }: { id: number; item: MateItem }) {
  const [grading, grade, clear] = useMateGrader(id);
  // **내가 낸 수만 들고 있다.** 玉方의 응수는 서버가 트리에서 꺼내 두므로, 화면이 그것을
  // 기억하면 두 벌이 되고 어긋날 수 있다(protocol/quiz.ts).
  const [mine, setMine] = useState<string[]>([]);
  /**
   * 이 문항을 몇 번 틀렸나. **「最初から」로 안 지워진다** — 지우면 세 번째 힌트에 영원히
   * 못 닿는다(다시 풀려면 그 버튼을 누르는 것이 유일한 길이라, 그때마다 0으로 돌아간다).
   */
  const [wrongs, setWrongs] = useState(0);

  const res = grading.result;
  const done = res?.outcome === 'solved' || res?.outcome === 'wrong';
  // **`not_check` 도 서버가 준 자리를 그대로 쓴다.** 그때 판은 안 움직였지만 그 자리는
  // **문제 국면이 아니라 지금까지 진행된 국면**이다 — 문항 쪽으로 되돌리면 맞힌 수가
  // 사라진 것처럼 보인다(quiz.GradeMate 가 그래서 그 경우에도 둘 수 있는 수를 준다).
  const sfen = res ? res.sfen : item.sfen;
  const legal = res ? (res.legalMoves ?? []) : item.legalMoves;
  const plies = res ? res.plies : item.plies;

  const play = (usi: string): void => {
    const next = [...mine, usi];
    // 시도 횟수는 **틀린 횟수**다. 王手가 아닌 수는 안내이지 오답이 아니라 세지 않는다 —
    // 서버가 그 자리에서 정답을 안 주는 이유와 같다(quiz.MateNotCheck).
    void grade({ moves: next, attempt: wrongs + 1 }).then((got) => {
      if (!got) return;
      if (got.outcome === 'wrong') setWrongs((n) => n + 1);
      // **오답과 王手 아님은 수를 쌓지 않는다.** 오답은 거기서 그 시도가 끝나고, 王手 아님은
      // 판이 그대로라 다시 두는 자리다.
      if (got.outcome === 'ongoing' || got.outcome === 'solved') setMine(next);
    });
  };

  const retry = (): void => {
    setMine([]);
    clear();
  };

  return (
    <article className="quiz-item" aria-label="詰みの問題">
      <header className="quiz-item-head">
        <h3>
          {item.plies}手詰め
          <span className="quiz-item-ply">{item.ply}手目の局面</span>
        </h3>
        {/* **놓친 詰み과 決めた 詰み은 사람에게 전혀 다른 이야기다.** 같은 문장으로 두면
            이미 이긴 사람에게 「놓쳤다」고 말하거나 그 반대가 된다. */}
        <p className="quiz-item-lead">
          {item.converted
            ? 'この対局であなたが決めた詰みです。もう一度指してみてください。'
            : 'この局面に詰みがありました。王手を続けて詰ませてください。'}
        </p>
      </header>

      <QuizBoard
        sfen={sfen}
        me={sideOf(item.sfen)}
        legalMoves={legal}
        checked={(res ? res.checked : item.checked) ?? null}
        // **판을 지금 모양으로 만든 수**를 짚는다. 오답이면 방금 낸 그 수이고, 정답이면
        // 玉方의 응수다 — 문장이 짚는 것과 같은 수라야 둘이 서로를 가리킨다.
        lastMove={squaresOf(res?.line.at(-1) ?? '')}
        interactive={!done && !grading.pending}
        onPlay={play}
      />

      <QuizVerdict
        message={grading.error ?? res?.message ?? null}
        tone={verdictTone(res, grading.error)}
        pending={grading.pending}
      />

      {!done && plies > 0 && (
        <p className="quiz-item-note">
          残り{plies}手。<strong>王手になる手だけ</strong>が指せます。
        </p>
      )}

      {(done || mine.length > 0) && (
        <button type="button" className="review-retry" onClick={retry}>
          最初から
        </button>
      )}
    </article>
  );
}

function verdictTone(res: MateResult | null, error: string | null): VerdictTone {
  if (error) return 'error';
  switch (res?.outcome) {
    case 'solved':
      return 'right';
    case 'wrong':
      return 'wrong';
    case 'not_check':
      return 'note';
    default:
      return res ? 'right' : 'none';
  }
}

/**
 * 「この局面の最善手は?」 문항.
 *
 * **첫 수만 받는다.** 그 뒤를 이어 두게 하면 「최선수인가」가 아니라 「그 수순이 좋은가」를
 * 묻는 다른 문항이 되고, 그것은 판정에 엔진이 다시 필요하다(§53).
 */
function BestQuestion({ id, item }: { id: number; item: BestItem }) {
  const [grading, grade, clear] = useBestGrader(id);
  /** 이 문항을 몇 번 틀렸나. **「もう一度」로 안 지워진다**(詰み 쪽과 같은 이유). */
  const [wrongs, setWrongs] = useState(0);
  const res = grading.result;
  /**
   * 맞혔는가. **cp 표가 여기 걸린다.**
   *
   * 채점 결과가 있으면 그 표를 그리던 자리다 — 그래서 오답 문구에서 정답을 지워도 그 표가
   * 정답과 두 cp를 그대로 적고 있었다. 이제 서버가 오답에는 그 값들을 안 보내고(§6 #10 ·
   * #11), 화면도 맞혔을 때만 그 자리를 만든다.
   */
  const solved = res?.correct === true;
  /**
   * 최선 수순에서 지금 보고 있는 자리. null이면 정답을 둔 국면이다.
   *
   * **판 하나를 그대로 쓴다.** 수순을 따로 그리는 작은 판을 세우면 같은 국면이 화면에 둘
   * 서고, 어느 쪽이 지금인지가 없어진다 — 개입 카드가 수순을 판 위에서 재생하는 것과
   * 같은 판단이다(§14).
   */
  const [peek, setPeek] = useState<number | null>(null);

  const play = (usi: string): void => {
    void grade({ index: item.index, move: usi, attempt: wrongs + 1 }).then((got) => {
      if (got && !got.correct) setWrongs((n) => n + 1);
    });
  };

  // **낸 수를 판에서 보여준다.** 서버가 그 수를 둔 뒤의 국면을 주므로 화면이 수를 두지
  // 않는다 — 화면은 규칙을 모른다. 못 만들었으면(`sfen` 이 빈 값) 문제 국면 그대로다.
  //
  // 문장만으로는 부족했던 자리다: 정답과 打 한 글자로만 갈리는 수를 낸 사람은 **무엇이
  // 등록됐는지 확인할 길이 화면에 하나도 없었다**(회차 1 #17·#18). 출발 칸이 빛나면 반상
  // 이동이고 안 빛나면 持ち駒에서 온 수라, 그 한 글자가 판 위에서 갈린다.
  const moved = res?.sfen ? { sfen: res.sfen, checked: res.checked ?? null, move: res.move } : null;

  /** 수순을 짚어 보는 중이면 그 국면이 판을 이긴다. */
  const line = solved ? (res.line ?? []) : [];
  const peeking = peek !== null ? line[peek] : undefined;

  /** 「もう一度」는 수순 짚기도 같이 되돌린다 — 판이 문제 국면으로 가는데 줄만 켜져 있으면 어긋난다. */
  const reset = (): void => {
    setPeek(null);
    clear();
  };

  return (
    <article className="quiz-item" aria-label="最善手の問題">
      <header className="quiz-item-head">
        <h3>
          最善手はどれ?
          <span className="quiz-item-ply">{item.ply}手目の局面</span>
        </h3>
        <p className="quiz-item-lead">この局面には、はっきり良い一手があります。</p>
      </header>

      {/* **판은 낸 수 뒤의 국면에서 멈춘다.** 빛나는 칸은 문제 국면의 합법수라, 그 국면을
          그린 채로 다시 두게 하면 판과 빛이 어긋난다 — 다시 푸는 길은 아래 「もう一度」이고
          그것이 판을 문제 국면으로 되돌린다. */}
      <QuizBoard
        sfen={peeking ? peeking.sfen : moved ? moved.sfen : item.sfen}
        me={sideOf(item.sfen)}
        legalMoves={res ? [] : item.legalMoves}
        checked={peeking ? null : moved ? moved.checked : (item.checked ?? null)}
        lastMove={peeking ? squaresOf(peeking.usi) : moved ? squaresOf(moved.move) : null}
        interactive={!res && !grading.pending}
        onPlay={play}
      />

      <QuizVerdict
        message={grading.error ?? res?.message ?? null}
        tone={grading.error ? 'error' : res ? (res.correct ? 'right' : 'wrong') : 'none'}
        pending={grading.pending}
      />

      {/* 두 cp는 **맞혔을 때만** 온다. 차가 이 문항이 뽑힌 기준이라 그것을 보여준다. */}
      {solved && res.answerCp !== undefined && res.secondCp !== undefined && (
        <dl className="quiz-scores">
          <div>
            <dt>最善手</dt>
            <dd>
              {res.answerJa || res.answer} ({signed(res.answerCp)})
            </dd>
          </div>
          <div>
            <dt>次善手</dt>
            <dd>{signed(res.secondCp)}</dd>
          </div>
        </dl>
      )}

      {/* 회차 2 #12. **정답만으로는 왜 최선인지가 안 보였다** — 「その手が捨てる手に見える」.
          누르면 판이 그 자리로 간다: 取り返す·逃げる가 거기서 눈에 들어온다.

          **추가 탐색이 0이다** — 문항을 만들 때 이미 손에 있던 수순을 잘라 둔 것이다.
          옛 판에는 그 칸이 없어서 이 줄이 통째로 안 뜬다. */}
      {line.length > 0 && (
        <div className="quiz-line">
          <span className="quiz-line__head">このあとの進み方</span>
          <ol className="quiz-line__moves">
            {line.map((m, i) => (
              <li key={m.usi}>
                <button
                  type="button"
                  className="quiz-line__move"
                  data-on={peek === i || undefined}
                  // 같은 수를 다시 누르면 정답을 둔 국면으로 돌아간다 — 짚어 보는 자리라
                  // 나가는 길이 들어온 길과 같아야 한다.
                  onClick={() => setPeek((cur) => (cur === i ? null : i))}
                >
                  {m.ja}
                </button>
              </li>
            ))}
          </ol>
        </div>
      )}

      {/* **맞혔는지에 따라 다른 말이다.** 맞힌 뒤의 「もう一度」는 다시 둬 보는 것이고,
          틀린 뒤의 그것은 아직 안 끝난 문항을 이어 푸는 자리다. */}
      {res && (
        <button type="button" className="review-retry" onClick={reset}>
          {solved ? 'もう一度' : 'もう一度考える'}
        </button>
      )}
    </article>
  );
}

/**
 * 문항의 국면에서 **사람이 잡은 쪽**을 얻는다.
 *
 * 문항은 늘 사람이 둘 차례인 국면에서 뽑히므로(quiz.bestItems·mateItem) 그 手番이 곧
 * 사람이다. **진행된 판이 아니라 문항의 국면을 본다** — 진행된 판은 답한 뒤 상대 차례가 된다.
 */
function sideOf(sfen: string): Side {
  try {
    return parseSfen(sfen).turn;
  } catch {
    return 'black';
  }
}

/** cp는 부호를 붙여 적는다 — 0과 「없다」가 구별되어야 하고, 부호가 곧 누가 좋은가다. */
function signed(cp: number): string {
  return cp > 0 ? `+${cp}` : String(cp);
}

type VerdictTone = 'none' | 'right' | 'wrong' | 'note' | 'error';

/**
 * 채점 결과 한 줄.
 *
 * **자리를 지킨다.** 문장이 있을 때만 그리면 답할 때마다 아래 것들이 밀려 올라가고, 그러면
 * 방금 무엇이 바뀌었는지를 눈이 못 따라간다(WhatIfPanel 이 같은 자리를 이미 밟았다).
 */
function QuizVerdict({ message, tone, pending }: { message: string | null; tone: VerdictTone; pending: boolean }) {
  return (
    <p className="quiz-verdict" data-tone={tone} role={tone === 'error' ? 'alert' : 'status'}>
      {pending ? '採点しています…' : (message ?? ' ')}
    </p>
  );
}

/**
 * 문항 하나의 판.
 *
 * 되짚기와 같은 `Board`·`Hand` 다. **화면은 규칙을 모른다** — 빛나는 칸은 서버가 준
 * `legalMoves` 가 정하고, 詰み 문항에서는 그 배열이 王手만 담고 있다.
 */
function QuizBoard({
  sfen,
  me,
  legalMoves,
  checked,
  lastMove,
  interactive,
  onPlay,
}: {
  sfen: string;
  /**
   * 사람이 잡은 쪽. **문항의 국면에서 얻어 밖에서 넘긴다.**
   *
   * 여기 판의 手番으로 다시 세면 안 된다 — 문항이 끝난 뒤의 판은 **상대 차례**라, 그때
   * 駒台의 이름이 뒤집혀 자기 駒台가 `相手` 가 된다. 판을 안 뒤집으므로(아래) 그 이름이
   * 누가 누구인지를 말하는 **유일한 자리**다(ReviewDetail 이 같은 이유로 `myColor` 를 쓴다).
   */
  me: Side;
  legalMoves: readonly string[];
  checked: string | null;
  /**
   * 이 판을 지금 모양으로 만든 한 수. 두 칸을 함께 짚는다.
   *
   * **打과 반상 이동이 여기서 갈린다** — 출발 칸이 빛나지 않으면 持ち駒에서 온 수다. 「▲3五金」과
   * 「▲3五金打」가 한 글자 차이라 문장만으로는 안 갈렸고, 그것이 회차 1 #17의 절반이었다.
   */
  lastMove: MoveSquares | null;
  interactive: boolean;
  onPlay: (usi: string) => void;
}) {
  const [origin, setOrigin] = useState<string | null>(null);
  const [promoting, setPromoting] = useState<{ origin: string; to: string } | null>(null);

  const board = useMemo<BoardModel | null>(() => {
    try {
      return parseSfen(sfen);
    } catch {
      return null;
    }
  }, [sfen]);

  // **판이 갈리면 고른 駒를 놓는다.** 「最初から」·「もう一度」는 위에서 상태를 되돌리는데
  // 이 컴포넌트는 그대로 살아 있어서, 고른 칸이 새 국면에서는 출발점이 아닌 채로 빛난다.
  const shown = useRef(sfen);
  if (shown.current !== sfen) {
    shown.current = sfen;
    if (origin !== null) setOrigin(null);
    if (promoting !== null) setPromoting(null);
  }

  const grouped = useMemo(() => groupByOrigin(legalMoves), [legalMoves]);
  const destinations: Destination[] = origin ? (grouped.get(origin) ?? []) : [];
  const lit = useMemo(() => new Set(destinations.map((d) => d.to)), [destinations]);
  const droppable = useMemo(
    () => (interactive ? new Set([...grouped.keys()].filter((o) => o.endsWith('*'))) : new Set<string>()),
    [interactive, grouped],
  );

  const send = (usi: string): void => {
    setOrigin(null);
    setPromoting(null);
    onPlay(usi);
  };

  const onSquare = (usi: string): void => {
    if (!interactive) return;
    if (origin && lit.has(usi)) {
      const dest = destinations.find((d) => d.to === usi);
      if (!dest) return;
      // 성·불성이 둘 다 되면 물어본다. 한쪽만 되면 그것으로 둔다 — 물어볼 것이 없다.
      if (dest.plain && dest.promote) {
        setPromoting({ origin, to: usi });
        return;
      }
      send(toUsiMove(origin, usi, dest.promote));
      return;
    }
    setOrigin(grouped.has(usi) && usi !== origin ? usi : null);
  };

  if (!board) {
    return <p className="review-broken">この局面を再現できません。</p>;
  }

  const pickHand = (next: string): void => {
    if (!interactive) return;
    setOrigin(next === origin ? null : next);
  };

  return (
    <div className="quiz-board">
      {/* 문항의 판은 **뒤집지 않는다.** 되짚기와 같은 방향이라야 같은 판을 보고 있다는 것이
          읽히고, 이 화면은 그 판에서 곧바로 건너온 자리다. */}
      <Hand
        side="white"
        label={me === 'white' ? 'あなた' : '相手'}
        pieces={board.hands.white}
        selected={origin?.endsWith('*') && board.turn === 'white' ? origin : null}
        playable={board.turn === 'white' ? droppable : new Set()}
        onPick={board.turn === 'white' ? pickHand : () => {}}
      />

      <Board
        board={board}
        lit={lit}
        selected={origin && !origin.endsWith('*') ? origin : null}
        lastMove={lastMove}
        checked={checked}
        played={null}
        replay={null}
        ray={null}
        motion={null}
        checks={[]}
        dimmed={false}
        dropFrom={null}
        hintSquare={null}
        hintRay={null}
        mateHeat={0}
        me={me}
        flipped={false}
        interactive={interactive}
        onSquare={onSquare}
      />

      <Hand
        side="black"
        label={me === 'black' ? 'あなた' : '相手'}
        pieces={board.hands.black}
        selected={origin?.endsWith('*') && board.turn === 'black' ? origin : null}
        playable={board.turn === 'black' ? droppable : new Set()}
        onPick={board.turn === 'black' ? pickHand : () => {}}
      />

      {/* **되짚기와 같은 물음이다.** 클래스도 어휘도 그쪽 것을 그대로 쓴다(`ReviewDetail`) —
          이름을 새로 지으면 스타일이 없는 맨 `div` 가 판 위에 서고, 그건 눈으로만 잡힌다. */}
      {promoting && (
        <div className="promotion" role="group" aria-label="成りの選択">
          <span>成りますか。</span>
          <button
            type="button"
            className="btn btn--primary"
            onClick={() => send(toUsiMove(promoting.origin, promoting.to, true))}
          >
            成る
          </button>
          <button type="button" className="btn" onClick={() => send(toUsiMove(promoting.origin, promoting.to, false))}>
            不成
          </button>
        </div>
      )}
    </div>
  );
}
