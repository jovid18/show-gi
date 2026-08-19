import { useProfile } from '@/hooks/useProfile';
import { TAG_KIND_JA } from '@/libs/game/tags';
import { SIGN_IN_PATH } from '@/protocol/auth';
import { hrefOf, navigate } from '@/routes/router';

/**
 * 마이페이지. **판을 가로질러 보는 유일한 화면이다** — 되짚기는 판 하나를 열고 총평은
 * 판 하나를 세지만, 여기는 「지금까지 어땠나」에 답한다.
 *
 * 넷을 그린다: 段級 · 전적 · 崩れやすいところ · 組んだ形. 그 이상은 안 그린다 — 이 화면은 사람이
 * 자기를 확인하러 오는 자리이고, 판마다의 이야기는 되짚기가 이미 한다.
 *
 * **마지막 하나가 나머지 셋과 방향이 반대다.** 앞의 셋은 「얼마나 못했나」를 세는데, 그것만
 * 있으면 판을 가로질러 보는 유일한 화면이 지적만 하는 자리가 된다(journal §77).
 *
 * **로그아웃도 여기 있다**(journal §86) — 「내 계정」을 여는 화면이 이미 있으면 그 안이
 * 그것의 자리다.
 */
export function ProfileScreen({ onSignOut }: { onSignOut: () => void }) {
  const state = useProfile();

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
        {/* **여기까지 온 사람에게 갈 곳을 준다.** 메뉴에서는 이 줄이 로그인한 사람에게만
            보이지만(HomeScreen), 주소를 직접 열거나 로그아웃한 뒤에는 익명으로 여기 선다 —
            그때 안내만 있고 누를 것이 없으면 막다른 화면이 된다. */}
        <p className="profile__signin">
          <a className="btn btn--primary" href={SIGN_IN_PATH}>
            ログイン
          </a>
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
                한쪽에만 두면 다른 쪽에서 그대로 오해가 산다(journal §62). */}
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
            まだ終わった対局がありません。{' '}
            {/* 안내 화면과 같은 이유로 `navigate` 를 탄다 — 문서를 새로 받으면 상시
                마운트된 대국 화면이 들고 있던 총평이 사라진다(GuideScreen). */}
            <a
              href={hrefOf({ name: 'game' })}
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                e.preventDefault();
                navigate({ name: 'game' });
              }}
            >
              対局する
            </a>
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

      {/* **「崩れやすいところ」의 반대편이다.** 저쪽이 무너진 자리라면 이쪽은 실제로 세운
          것이고, 마이페이지가 지적만 하는 화면이 되지 않게 하는 것이 이 절의 몫이다. */}
      <section className="profile__block" aria-label="組んだ形">
        <h2 className="profile__head">組んだ形</h2>
        {profile.styles && profile.styles.length > 0 ? (
          <ul className="profile__styles">
            {profile.styles.map((s) => (
              <li key={s.code}>
                {/* 축을 먼저 적는다 — 「美濃囲い」와 「中飛車」가 한 목록에 서므로,
                    없으면 둘이 같은 종류로 읽힌다. 대국 중의 알림과 같은 표다. */}
                <span className="profile__styles-kind">{TAG_KIND_JA[s.kind]}</span>
                <span className="profile__styles-name">{s.nameJa}</span>
                {/* **「回」가 아니라 「局」이다.** 한 판에 같은 이름은 한 번만 담긴다. */}
                <span className="profile__styles-count">{s.games}局</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="profile__empty">
            まだ名前のついた形がありません。囲いや戦法が組み上がると、その場で盤に名前が出ます。
          </p>
        )}
      </section>

      {/* **맨 아래이고, 눈에 띄지 않는다.** 이 화면에서 사람이 보러 온 것은 위의 넷이고
          이건 가끔 한 번 쓰는 것이라, 위에 두면 성적을 보러 온 사람이 매번 지나친다. */}
      <div className="profile__signout">
        <button type="button" className="btn" onClick={onSignOut}>
          ログアウト
        </button>
      </div>
    </section>
  );
}
