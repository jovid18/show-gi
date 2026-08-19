import { SIGN_IN_PATH, type MeResponse } from '@/protocol/auth';
import { hrefOf, navigate, type Route } from '@/routes/router';

/**
 * 홈. **갈 수 있는 곳을 세로로 한 줄씩 세운 메뉴가 전부다**(journal §86).
 *
 * **서버에 아무것도 안 묻는다.** 로그인 여부는 이미 App이 들고 있는 것을 받아 쓴다 —
 * 여기서 한 번 더 물으면 첫 화면에 요청이 하나 늘고, 늘어난 만큼 메뉴가 늦게 선다.
 */

interface MenuItem {
  route: Route;
  name: string;
  note: string;
  /** 눈에 먼저 들어오는 한 줄. **대국 하나뿐이다** — 둘이 되면 어느 쪽도 첫째가 아니다. */
  primary?: boolean;
  /** 로그인한 사람에게만 그린다. 눌러도 401인 줄을 세우면 고장으로 읽힌다(journal §76). */
  needsAuth?: boolean;
  /**
   * 두는 중에는 안 그린다. 지금은 검토 하나다 — 이유는 아래 `MENU`.
   *
   * **두는 중에 이 화면이 보이는 일은 사실 없다**(App.tsx가 판으로 되돌린다). 그래도
   * 남는 것은 되돌리기가 그리고 난 뒤라, 라우트가 갈리는 한 틱 동안 이 목록이 실제로
   * 그려지기 때문이다.
   */
  hideWhilePlaying?: boolean;
}

const MENU: MenuItem[] = [
  { route: { name: 'game' }, name: '対局', note: 'コンピュータと一局指す', primary: true },
  { route: { name: 'reviews' }, name: '振り返り', note: '終わった対局を見直す' },
  // **두는 중에는 줄 자체가 사라진다** — 그 벽이 왜 있는지는 01-core.md §1. 화면
  // 쪽에도 같은 벽이 있다(ExploreScreen). 링크와 새로고침으로 들어오는 길이 남는다.
  {
    route: { name: 'explore', handicap: '', moves: [] },
    name: '検討',
    note: '好きな局面を並べて調べる',
    needsAuth: true,
    hideWhilePlaying: true,
  },
  // **여기가 안내의 유일한 입구다**(journal §86). 새 탭으로 안 연다 — 메뉴의 다른
  // 줄과 같은 탭으로 간다.
  { route: { name: 'guide' }, name: 'あそびかた', note: 'このアプリの遊びかた' },
  // **로그인해야 뜬다.** 익명에게는 401인 화면이라(profile.go) 줄만 서 있으면 눌러서
  // 빈 화면을 보게 된다 — 그 자리는 아래 ログイン 줄이 맡는다(journal §76).
  { route: { name: 'me' }, name: 'マイページ', note: '成績と棋力の目安', needsAuth: true },
];

export function HomeScreen({ me, playing }: { me: MeResponse; playing: boolean }) {
  return (
    <section className="home">
      <div className="home__hero">
        {/* 파비콘·헤더와 **같은 파일**이다(brand/icons.sh). 96px 원본을 64로 줄여 그린다.
            바로 아래 제품 이름이 글자로 서 있으므로 `alt` 는 비운다. */}
        <img
          className="home__mark"
          src="/logo-96.png"
          alt=""
          width={64}
          height={64}
          fetchPriority="high"
          decoding="async"
        />
        <h1 className="home__title">show-gi</h1>
        <p className="home__tagline">口を出すときを自分で決める将棋の相手</p>
      </div>

      <nav className="home__menu" aria-label="メニュー">
        {MENU.filter((item) => (!item.needsAuth || me.user !== null) && !(item.hideWhilePlaying && playing)).map(
          (item) => {
            // **버튼이 아니라 링크다.** 주소가 화면을 정하므로 가운데 클릭·링크 복사·
            // 새 탭이 그냥 동작해야 하고, 그건 `<a href>` 만이 준다.
            return (
              <a
                key={item.route.name}
                className="home__item"
                href={hrefOf(item.route)}
                data-primary={item.primary || undefined}
                onClick={(e) => {
                  // 새 탭·새 창으로 열려는 클릭은 브라우저에 넘긴다
                  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                  e.preventDefault();
                  navigate(item.route);
                }}
              >
                <span className="home__item-body">
                  <span className="home__item-name">{item.name}</span>
                  <span className="home__item-note">{item.note}</span>
                </span>
                <span className="home__item-arrow" aria-hidden="true">
                  →
                </span>
              </a>
            );
          },
        )}

        {/* 로그인. 로그인한 사람에게는 이 줄이 없다 — 그 자리는 マイページ가 대신한다.

            **다른 줄과 달리 `navigate` 를 안 탄다.** 브라우저를 통째로 Google로 보내는
            이동이라 화면 안의 라우팅으로는 못 간다. 로그인이 없는 배포에서는 줄 자체가
            없다(`me.enabled`) — 눌러도 안 되는 것을 띄우면 고장으로 읽힌다. */}
        {me.enabled && me.user === null && (
          <a className="home__item" href={SIGN_IN_PATH}>
            <span className="home__item-body">
              <span className="home__item-name">ログイン</span>
              <span className="home__item-note">対局の記録が残り、成績を見られます</span>
            </span>
            <span className="home__item-arrow" aria-hidden="true">
              →
            </span>
          </a>
        )}
      </nav>
    </section>
  );
}
