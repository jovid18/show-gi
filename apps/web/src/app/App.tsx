import { lazy, Suspense, useEffect } from 'react';

import { Account } from '@/components/Account';
import { useViewer } from '@/hooks/useViewer';
import { GameScreen } from '@/screens/game/GameScreen';
import { hrefOf, navigate, useRoute, type Route } from '@/routes/router';

/**
 * 되짚기는 **나중에 받는다.**
 *
 * 이 화면이 평가치 궤적 때문에 차트 라이브러리를 끌고 오는데(recharts), 그게 첫 화면에 같이
 * 실리면 **대국을 열려고 온 사람이 되짚기용 코드를 먼저 내려받는다.** 라우트가 이미 갈라져
 * 있어서(`/reviews`) 떼어내는 데 드는 것이 이 세 줄이다.
 */
const ReviewScreen = lazy(async () => ({ default: (await import('@/screens/review/ReviewScreen')).ReviewScreen }));

/**
 * 퀴즈도 나중에 받는다. **되짚기와 갈라 둔다** — 판 하나를 보러 온 사람이 퀴즈 화면까지
 * 내려받을 이유가 없고, 라우트가 이미 갈라져 있어서 떼는 데 드는 것이 이 두 줄이다.
 */
const QuizScreen = lazy(async () => ({ default: (await import('@/screens/quiz/QuizScreen')).QuizScreen }));

/** 마이페이지도 나중에 받는다. 첫 화면은 대국이고 여기는 판 하나도 안 그린다. */
const ProfileScreen = lazy(async () => ({
  default: (await import('@/screens/me/ProfileScreen')).ProfileScreen,
}));

/**
 * 안내도 나중에 받는다. **여기는 글이 전부**라 첫 화면에 실릴 이유가 가장 적고,
 * 검색에서 들어오는 사람은 이 조각 하나만 받으면 된다.
 */
const GuideScreen = lazy(async () => ({
  default: (await import('@/screens/guide/GuideScreen')).GuideScreen,
}));

/**
 * `needsAuth` 는 **실제로 로그인한 사람에게만** 그리는 탭이다.
 *
 * 마이페이지는 익명에게 401이라(profile.go) 안 누른 사람에게는 갈 곳이 없다 —
 * 「눌러도 안 되는 버튼을 띄우면 고장으로 읽힌다」가 `Account` 에 이미 있는 규칙이고,
 * 탭이라고 다르지 않다.
 *
 * **로그인 기능이 켜졌는가(`me.enabled`)로 가르지 않는다.** 그 값은 배포에 로그인이
 * 있는가일 뿐이라 익명으로 두는 사람에게도 참이고, 실제로 그 탭이 익명에게 떠 있었다
 * (06-status.md §76).
 */
const TABS: { route: Route; label: string; needsAuth?: boolean }[] = [
  { route: { name: 'game' }, label: '対局' },
  { route: { name: 'reviews' }, label: '振り返り' },
  { route: { name: 'me' }, label: 'マイページ', needsAuth: true },
  // **로그인을 안 본다.** 처음 온 사람이 읽는 자리라 오히려 익명일 때 가장 필요하다.
  { route: { name: 'guide' }, label: 'あそびかた' },
];

/**
 * 어느 탭에 불이 들어오나. **화면 이름과 탭 이름이 1:1이 아니다** — 되짚기 탭 하나가
 * 목록·상세·퀴즈 셋을 맡는다.
 *
 * 삼항으로 이어 쓰던 자리인데, 갈래가 넷이 되면서 그 사슬이 「나머지 전부」를 되짚기로
 * 보내 **안내 화면에서 되짚기 탭에 불이 들어왔다.** 대응을 표로 적으면 갈래가 늘어도
 * 마지막 항이 남의 것을 삼키지 않는다.
 */
function activeTabOf(route: Route): Route['name'] {
  switch (route.name) {
    case 'review':
    case 'quiz':
      return 'reviews';
    default:
      return route.name;
  }
}

/**
 * 화면마다 다른 제목. **검색 결과에 나가는 줄이고**, 탭을 여러 개 열어 둔 사람이 어느
 * 것이 무엇인지 구분하는 줄이기도 하다.
 *
 * `index.html` 의 것과 **어휘를 맞춘다** — 저쪽이 첫 로드와 크롤러가 보는 값이고 여기는
 * 그 뒤의 이동이라, 같은 화면이 두 이름을 가지면 안 된다.
 */
const TITLE_JA: Record<Route['name'], string> = {
  game: '対局 | show-gi',
  reviews: '振り返り | show-gi',
  review: '振り返り | show-gi',
  quiz: 'クイズ | show-gi',
  me: 'マイページ | show-gi',
  guide: 'あそびかた | show-gi',
};

/**
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  const route = useRoute();
  const onGame = route.name === 'game';
  const onMe = route.name === 'me';
  const onGuide = route.name === 'guide';
  const activeTab = activeTabOf(route);
  const { me, signOut } = useViewer();

  useEffect(() => {
    document.title = TITLE_JA[route.name];
  }, [route.name]);

  return (
    <>
      {/* 화면 전폭을 쓰고 스크롤에 붙어 있는다. **겹은 장막(40)보다 낮다** — 개입이
          걸리면 이 줄도 방과 함께 어두워져야 하고, 위에 있으면 판만 남는 그림이 깨진다. */}
      <header className="app-head">
        <div className="app-head__inner">
          <a className="app-brand" href={hrefOf({ name: 'game' })}>
            {/* 마크. **파일은 `public/` 에 있다**(brand/icons.sh가 로고 한 장에서 만든다) —
                번들에 넣으면 파비콘·홈 화면 아이콘과 같은 그림이 두 벌로 나간다.
                옆에 제품 이름이 글자로 서 있으므로 `alt` 는 비운다. */}
            <img
              className="app-mark"
              src="/logo-96.png"
              alt=""
              width={30}
              height={30}
              // 헤더는 첫 화면에서 언제나 보이는 자리라 미루지 않는다.
              fetchPriority="high"
              decoding="async"
            />
            <span className="app-brand__text">
              <span className="app-title">show-gi</span>
              <span className="app-tagline">口を出すときを自分で決める将棋の相手</span>
            </span>
          </a>

          <nav className="app-tabs" aria-label="画面">
            {TABS.filter((tab) => !tab.needsAuth || me.user !== null).map((tab) => {
              // **버튼이 아니라 링크다.** 주소가 화면을 정하므로 가운데 클릭·링크 복사·
              // 새 탭이 그냥 동작해야 하고, 그건 `<a href>` 만이 준다.
              // 켜지는 조건은 `activeTabOf` 가 안다 — 되짚기 탭 하나가 셋(목록·상세·퀴즈)을
              // 맡기 때문에 화면 이름을 그대로 견줄 수 없다.
              const active = tab.route.name === activeTab;
              return (
                <a
                  key={tab.route.name}
                  className="app-tab"
                  href={hrefOf(tab.route)}
                  data-active={active || undefined}
                  aria-current={active ? 'page' : undefined}
                  onClick={(e) => {
                    // 새 탭·새 창으로 열려는 클릭은 브라우저에 넘긴다
                    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                    e.preventDefault();
                    navigate(tab.route);
                  }}
                >
                  {tab.label}
                </a>
              );
            })}
          </nav>

          <Account me={me} onSignOut={signOut} />
        </div>
      </header>

      <div className="app">
        {/*
          **대국 화면은 감추기만 한다.** 대국은 WebSocket 연결 하나에 매여 있어서, 여기서
          컴포넌트를 내리면 연결이 끊기고 그 판은 `abandoned` 로 닫힌다 — 두던 판을 두고
          지난 판을 보러 갔다가 돌아오면 판이 사라져 있는 것이 된다.

          그래서 이 자리는 **라우트 밖**이다(libs/router.ts).
        */}
        <div hidden={!onGame}>
          <GameScreen />
        </div>

        {/* 리뷰는 열 때마다 새로 부른다. 방금 끝난 판이 목록 맨 위에 있어야 한다. */}
        {!onGame && (
          <Suspense fallback={<p className="review-status">読み込み中…</p>}>
            {onGuide ? (
              <GuideScreen />
            ) : onMe ? (
              <ProfileScreen />
            ) : route.name === 'quiz' ? (
              // **판마다 새로 세운다.** `id` 만 갈아 끼우면 이 컴포넌트가 그대로 살아서
              // 앞 판의 답과 기다린 횟수를 물려받고, 한 틱 동안 **남의 문항**을 그린다.
              <QuizScreen key={route.id} id={route.id} />
            ) : (
              <ReviewScreen route={route} />
            )}
          </Suspense>
        )}
      </div>
    </>
  );
}
