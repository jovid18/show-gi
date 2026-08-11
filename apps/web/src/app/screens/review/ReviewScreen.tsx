import { ReviewDetail } from './ReviewDetail';
import { dateJa, resultJa } from '@/libs/review/labels';
import { hrefOf, navigate, type Route } from '@/routes/router';
import type { GameSummary } from '@/protocol/review';
import { useGameDetail, useGameList } from '@/hooks/useReview';

/**
 * 끝난 판을 되짚는 화면.
 *
 * **어느 판을 보고 있는지는 주소가 정한다**(`/reviews/12`). 화면 안의 상태로 두면
 * 새로고침과 뒤로 가기가 목록으로 튕기고, 그 판을 남에게 보여줄 링크가 없다.
 *
 * **대국 화면은 그대로 살아 있다** — App이 감춰만 두므로, 두던 판이 있으면 돌아왔을 때
 * 이어서 둘 수 있다.
 */
export function ReviewScreen({ route }: { route: Route }) {
  if (route.name === 'review') {
    return <SelectedGame id={route.id} />;
  }
  return <GameList />;
}

function GameList() {
  const { loaded, reload } = useGameList();

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
      </p>
    );
  }
  // **한 수도 안 둔 판은 서버가 이미 걸렀다**(review.go). 여기가 비면 정말로 없는 것이다.
  if (loaded.data.length === 0) {
    return <p className="review-status">まだ対局の記録がありません。一局指してみてください。</p>;
  }

  return (
    <section className="review-list" aria-label="対局一覧">
      <h2 className="panel-title">対局一覧</h2>
      <ul>
        {loaded.data.map((game) => (
          <li key={game.id}>
            {/* **링크다.** 판 하나가 주소 하나라, 새 탭으로 열고 링크를 복사할 수 있어야 한다. */}
            <a
              className="review-card"
              href={hrefOf({ name: 'review', id: game.id })}
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                e.preventDefault();
                navigate({ name: 'review', id: game.id });
              }}
            >
              <GameCard game={game} />
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}

function GameCard({ game }: { game: GameSummary }) {
  return (
    <>
      <time className="review-card-date" dateTime={game.startedAt}>
        {dateJa(game.startedAt)}
      </time>
      <span className="review-card-result" data-result={game.result}>
        {resultJa(game.result)}
      </span>
      <span className="review-card-moves">{game.moveCount}手</span>
      {/* 0회도 적는다. **止まらなかった것도 성적이다** — 빈 자리로 두면 셌는지조차 안 보인다. */}
      <span className="review-card-iv" data-none={game.interventionCount === 0 || undefined}>
        介入 {game.interventionCount}回
      </span>
    </>
  );
}

// 목록으로 돌아가는 것도 주소를 바꾸는 일이다 — 뒤로 가기와 같은 자리에 서야 한다.
const toList = (): void => navigate({ name: 'reviews' });

function SelectedGame({ id }: { id: number }) {
  const { loaded, reload } = useGameDetail(id);

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
        <button type="button" className="review-retry" onClick={toList}>
          対局一覧へ
        </button>
      </p>
    );
  }
  return <ReviewDetail game={loaded.data} onBack={toList} />;
}
