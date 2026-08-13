import { useProfile } from '@/hooks/useProfile';
import { hrefOf } from '@/routes/router';

/**
 * 마이페이지. **판을 가로질러 보는 유일한 화면이다** — 되짚기는 판 하나를 열고 총평은
 * 판 하나를 세지만, 여기는 「지금까지 어땠나」에 답한다.
 *
 * 셋을 그린다: 段級 · 전적 · 약점. 그 이상은 안 그린다 — 이 화면은 사람이 자기를 확인하러
 * 오는 자리이고, 판마다의 이야기는 되짚기가 이미 한다.
 */
export function ProfileScreen({ active }: { active: boolean }) {
  const state = useProfile(active);

  if (state.status === 'loading') return <p className="review-status">読み込み中…</p>;

  // **로그인 안 한 것은 오류가 아니다.** 익명 판은 서로 구별할 수단이 없어서 이 화면이
  // 답할 것이 애초에 없다 — 그러니 「失敗しました」가 아니라 무엇을 하면 되는지를 쓴다.
  if (state.status === 'anonymous') {
    return (
      <section className="profile">
        <p className="review-status">
          ログインすると、これまでの成績と棋力の目安が見られます。
          <br />
          ログインしていない対局は記録が誰のものか分からないため、ここには出ません。
        </p>
      </section>
    );
  }

  if (state.status === 'error') return <p className="review-status">成績を読み込めませんでした。</p>;

  const { profile } = state;

  return (
    <section className="profile">
      <h1 className="profile__name">{profile.name}</h1>

      <section className="profile__block" aria-label="棋力の目安">
        <h2 className="profile__head">棋力の目安</h2>
        {profile.rank ? (
          <>
            <p className="profile__rank">{profile.rank.nameJa}</p>
            <span className="rank__bar" aria-hidden="true">
              <i style={{ width: `${(profile.rank.step / profile.rank.max) * 100}%` }} />
            </span>
            {/* 총평과 **같은 문장**이다. 段級을 공인된 실력으로 읽지 않게 하는 한 줄이라
                한쪽에만 두면 다른 쪽에서 그대로 오해가 산다(06-status.md §62). */}
            <p className="profile__note">指し手の精度から算出した目安です。道場や将棋ウォーズの段級とは異なります。</p>
          </>
        ) : (
          <p className="profile__empty">まだ測っていません。何局か指すと出ます。</p>
        )}
      </section>

      <section className="profile__block" aria-label="成績">
        <h2 className="profile__head">成績</h2>
        {profile.record.games > 0 ? (
          <dl className="profile__record">
            <div>
              <dt>対局</dt>
              <dd>{profile.record.games}</dd>
            </div>
            <div>
              <dt>勝ち</dt>
              <dd>{profile.record.win}</dd>
            </div>
            <div>
              <dt>負け</dt>
              <dd>{profile.record.loss}</dd>
            </div>
            <div>
              <dt>引き分け</dt>
              <dd>{profile.record.draw}</dd>
            </div>
          </dl>
        ) : (
          <p className="profile__empty">
            まだ終わった対局がありません。<a href={hrefOf({ name: 'game' })}>対局する</a>
          </p>
        )}
      </section>

      <section className="profile__block" aria-label="崩れやすいところ">
        {/* **「弱点」이라고 안 쓴다.** 사람에 대한 판정으로 읽히는 낱말이고, 세는 것은
            어디까지나 「어떤 수에서 몇 번 물러졌나」다. */}
        <h2 className="profile__head">崩れやすいところ</h2>
        {profile.weaknesses && profile.weaknesses.length > 0 ? (
          <ul className="profile__weak">
            {profile.weaknesses.map((w) => (
              <li key={w.code}>
                <span className="profile__weak-name">{w.nameJa}</span>
                {/* 막대는 **비율**이고 숫자는 횟수다. 둘 다 두는 것은 「12回」가 많은지
                    적은지를 횟수만으로는 못 읽기 때문이다. */}
                <span className="profile__weak-bar" aria-hidden="true">
                  <i style={{ width: `${w.share * 100}%` }} />
                </span>
                <span className="profile__weak-count">{w.count}回</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="profile__empty">
            {profile.interventions > 0
              ? '同じところで繰り返し戻したことは、まだありません。'
              : 'まだ戻した手がありません。'}
          </p>
        )}
      </section>
    </section>
  );
}
