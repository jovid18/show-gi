import type { GameSummary, SkillRank } from '@/protocol/game';

/**
 * 段級 하나를 눈금과 이름으로 그린다.
 *
 * 눈금이 17칸이라 하나하나가 가늘다 — 그래서 막대 하나에 채운 만큼만 칠한다. 게이지를
 * 도트로 그리는 相手の強さ(5칸)와 다른 이유는 칸 수뿐이다.
 */
function Rank({ rank, label }: { rank: SkillRank; label: string }) {
  const filled = rank.max > 0 ? (rank.step / rank.max) * 100 : 0;
  return (
    <div className="rank">
      <span className="rank__label">{label}</span>
      <span className="rank__name">{rank.nameJa}</span>
      <span className="rank__bar" aria-hidden="true">
        <i style={{ width: `${filled}%` }} />
      </span>
    </div>
  );
}

/**
 * 이 판에서 棋力の目安가 어떻게 움직였나.
 *
 * **「目安」라고 적는 것이 이 블록에서 제일 중요한 한 글자다.** 우리가 아는 것은 임계치에
 * 대한 낙폭뿐이고(`skill.Estimate`) 그것을 道場や将棋ウォーズ의 段級에 맞춰 본 적이 없다 —
 * 그냥 「8級」이라고 쓰면 초심자는 그것을 공인된 실력으로 읽는다.
 *
 * **before 가 없으면 화살표를 안 그린다.** 첫 판이면 잰 적이 없고, 그때 기준선을 「시작할
 * 때의 실력」으로 그리면 아무도 안 잰 숫자가 사람에 대한 판정으로 선다.
 */
function SkillChange({ skill }: { skill: NonNullable<GameSummary['skill']> }) {
  return (
    <section className="skill-change" aria-label="棋力の目安">
      <h3 className="skill-change__head">棋力の目安</h3>
      {skill.before ? (
        <div className="skill-change__pair">
          <Rank rank={skill.before} label="対局前" />
          <span className="skill-change__arrow" aria-hidden="true">
            →
          </span>
          <Rank rank={skill.after} label="対局後" />
        </div>
      ) : (
        <div className="skill-change__pair skill-change__pair--single">
          <Rank rank={skill.after} label="今の目安" />
        </div>
      )}
      <p className="skill-change__note">指し手の精度から算出した目安です。道場や将棋ウォーズの段級とは異なります。</p>
    </section>
  );
}

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

          {/* 段級은 **맨 아래**다. 판이 어땠는지를 읽은 뒤에 오는 것이 순서이고, 위에 두면
              사람이 문장을 읽기 전에 자기 등급부터 본다. */}
          {summary.skill && <SkillChange skill={summary.skill} />}
        </>
      )}
    </section>
  );
}
