import type { GameSummary } from '@/protocol/game';

/**
 * 대국이 끝난 뒤의 총평.
 *
 * **문장과 숫자를 갈라 그린다.** 문장은 판이 어떤 모양이었는지를 말하고 숫자는 표로 선다 —
 * 같은 수를 두 곳에 두면 어긋났을 때 어느 쪽이 맞는지 알 수 없어서, 서버가 애초에 LLM에
 * 숫자를 주지 않는다(`explain.GameFacts`).
 *
 * **아직 안 온 동안에도 자리를 잡는다.** LLM을 기다려 몇 초 늦게 오는데, 그때 자리가 없으면
 * 문장이 도착하는 순간 아래 버튼들이 밀려 내려가 누르던 손이 어긋난다.
 *
 * `tier` 는 그리지 않는다 — 그 문장이 캐시에서 왔는지 LLM에서 왔는지는 만든 쪽의 사정이고,
 * 읽는 사람에게는 같은 문장이다.
 */
export function Summary({ summary }: { summary: GameSummary | null }) {
  return (
    <section className="summary" aria-label="この対局のふりかえり">
      <h2 className="summary__head">ふりかえり</h2>

      {summary === null ? (
        <p className="summary__waiting" role="status">
          まとめています…
        </p>
      ) : (
        <>
          <p className="summary__body">{summary.body}</p>

          <dl className="summary__stats">
            <div>
              <dt>あなたの手数</dt>
              <dd>{summary.stats.playerMoves}</dd>
            </div>
            <div>
              <dt>戻した回数</dt>
              <dd>{summary.stats.interventions}</dd>
            </div>
          </dl>

          {/* 카테고리는 **서버가 정한 순서**를 그대로 그린다. 화면이 다시 세면 문장이
              말하는 1위와 표의 1위가 갈릴 수 있다. */}
          {summary.stats.categories && summary.stats.categories.length > 0 && (
            <ul className="summary__cats">
              {summary.stats.categories.map((c) => (
                <li key={c.code}>
                  <span className="summary__cat-name">{c.nameJa}</span>
                  <span className="summary__cat-count">{c.count}</span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}
