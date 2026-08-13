import { useMemo, useState } from 'react';

import { Board } from '@/components/Board';
import { Hand } from '@/components/Hand';
import { groupByOrigin, toUsiMove, type Destination } from '@/libs/game/moves';
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
        <p className="quiz-status">
          この対局からは問題が作れませんでした。詰みの筋がなく、はっきりした一手のある局面もありませんでした。
        </p>
      ) : null}

      {quiz.mate && <MateQuestion id={id} item={quiz.mate} />}
      {best.map((item) => (
        <BestQuestion key={item.index} id={id} item={item} />
      ))}
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
    void grade({ moves: next }).then((got) => {
      if (!got) return;
      // **오답과 王手 아님은 수를 쌓지 않는다.** 오답은 거기서 문항이 끝나고, 王手 아님은
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
        legalMoves={legal}
        checked={res?.checked ?? null}
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
  const res = grading.result;

  return (
    <article className="quiz-item" aria-label="最善手の問題">
      <header className="quiz-item-head">
        <h3>
          最善手はどれ?
          <span className="quiz-item-ply">{item.ply}手目の局面</span>
        </h3>
        <p className="quiz-item-lead">この局面には、はっきり良い一手があります。</p>
      </header>

      <QuizBoard
        sfen={item.sfen}
        legalMoves={res ? [] : item.legalMoves}
        checked={item.checked ?? null}
        interactive={!res && !grading.pending}
        onPlay={(usi) => void grade({ index: item.index, move: usi })}
      />

      <QuizVerdict
        message={grading.error ?? res?.message ?? null}
        tone={grading.error ? 'error' : res ? (res.correct ? 'right' : 'wrong') : 'none'}
        pending={grading.pending}
      />

      {/* 두 cp는 채점 뒤에만 온다. **차가 이 문항이 뽑힌 기준**이라 그것을 보여준다. */}
      {res && (
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

      {res && (
        <button type="button" className="review-retry" onClick={clear}>
          もう一度
        </button>
      )}
    </article>
  );
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
  legalMoves,
  checked,
  interactive,
  onPlay,
}: {
  sfen: string;
  legalMoves: readonly string[];
  checked: string | null;
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

  // **사람은 늘 이 국면의 수번이다** — 문항이 그렇게 뽑힌다(quiz.bestItems·mateItem).
  // 그래서 판이 스스로 「누가 나인가」를 말할 수 있다. 이 값을 `black` 으로 박아 두면
  // 後手로 둔 판에서 **「相手の利き」 그늘이 반대쪽 기준으로 깔린다.**
  const me: Side = board.turn;

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
        lastMove={null}
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

      {promoting && (
        <div className="promote-ask" role="dialog" aria-label="成りますか">
          <p>成りますか?</p>
          <button type="button" onClick={() => send(toUsiMove(promoting.origin, promoting.to, true))}>
            成る
          </button>
          <button type="button" onClick={() => send(toUsiMove(promoting.origin, promoting.to, false))}>
            成らない
          </button>
        </div>
      )}
    </div>
  );
}
