import { useState } from 'react';

import { GameReview } from './GameReview';
import { dateJa, resultJa } from '@/review/labels';
import type { GameSummary } from '@/review/protocol';
import { useGameDetail, useGameList } from '@/review/useReview';

/**
 * 끝난 판을 되짚는 화면.
 *
 * 목록에서 판을 고르면 그 판으로 들어간다. **대국 화면은 그대로 살아 있다** — App이
 * 감춰만 두므로, 두던 판이 있으면 돌아왔을 때 이어서 둘 수 있다.
 */
export function ReviewScreen() {
  const [selected, setSelected] = useState<number | null>(null);

  if (selected !== null) {
    return <SelectedGame id={selected} onBack={() => setSelected(null)} />;
  }
  return <GameList onSelect={setSelected} />;
}

function GameList({ onSelect }: { onSelect: (id: number) => void }) {
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
            <button type="button" className="review-card" onClick={() => onSelect(game.id)}>
              <GameCard game={game} />
            </button>
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

function SelectedGame({ id, onBack }: { id: number; onBack: () => void }) {
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
        <button type="button" className="review-retry" onClick={onBack}>
          対局一覧へ
        </button>
      </p>
    );
  }
  return <GameReview game={loaded.data} onBack={onBack} />;
}
