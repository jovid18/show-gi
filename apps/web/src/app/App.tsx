import { lazy, Suspense, useEffect, useSyncExternalStore } from 'react';

import { Account } from '@/components/Account';
import { useViewer } from '@/hooks/useViewer';
import { getPlaying, subscribePlaying } from '@/libs/game/playing';
import { GameScreen } from '@/screens/game/GameScreen';
import { ROUTE_GUIDE } from '@/routes/const';
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
 * 대인전도 나중에 받는다. **엔진 대국과 코드를 안 나눠 쓴다** — 개입도 힌트도 없는
 * 화면이라(docs/journal §83) 첫 화면에 실릴 이유가 없고, 라우트가 이미 갈라져 있다.
 */
const MatchScreen = lazy(async () => ({
  default: (await import('@/screens/match/MatchScreen')).MatchScreen,
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
 * (journal §76).
 */
const TABS: { route: Route; label: string; needsAuth?: boolean }[] = [
  { route: { name: 'game' }, label: '対局' },
  { route: { name: 'reviews' }, label: '振り返り' },
  { route: { name: 'me' }, label: 'マイページ', needsAuth: true },
];

/**
 * 어느 탭에 불이 들어오나. **화면 이름과 탭 이름이 1:1이 아니다** — 되짚기 탭 하나가
 * 목록·상세·퀴즈 셋을 맡고, 안내는 탭이 아예 없다(아래 `.app-help`).
 *
 * 삼항으로 이어 쓰던 자리인데, 갈래가 늘면서 그 사슬이 「나머지 전부」를 되짚기로
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
  room: '対人戦 | show-gi',
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

  /**
   * 두는 중인가(`libs/game/playing.ts`). **대국 화면을 벗어났을 때만 쓴다.**
   *
   * 판을 두다가 되짚기로 옮기면 판이 화면에서 사라지는데 **연결도 국면도 그대로 살아
   * 있다** — 대국 화면은 언마운트되지 않고 감춰지기만 한다(아래 `hidden`). 그 사실을
   * 아무것도 말해 주지 않아서 「내 판이 날아갔나」로 읽히던 자리다.
   *
   * **확인창을 안 띄우는 이유가 그것이다.** 나가도 잃는 것이 없으므로 물으면 거짓이 되고,
   * 눌러도 되는 창은 다음에 진짜로 물어야 할 때 같이 무시된다. 사실을 그대로 표시한다.
   */
  const playing = useSyncExternalStore(subscribePlaying, getPlaying);

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

          {/*
            **탭이 아니라 브랜드 옆의 별도 버튼이고, 새 탭으로 연다.**

            안내를 보고 싶어지는 때가 「지금 무슨 일이 일어났나」인데 그건 대국 중이다.
            같은 탭에서 열면 판이 화면에서 사라진다 — 대국은 라우트 밖이라 연결도 판도
            살아 있지만(아래 `hidden`), **보이지 않는 것과 없는 것은 사람에게 같다.**
            새 탭이면 판을 옆에 두고 읽는다.

            그래서 탭 무리에서 뺐다. 저쪽은 「지금 어느 화면인가」를 말하는 자리이고,
            이건 화면을 안 바꾸므로 같은 무리에 서면 거짓말이 된다.
          */}
          <a
            className="app-help"
            href={ROUTE_GUIDE}
            target="_blank"
            rel="noreferrer noopener"
            // 새 탭으로 연다는 것은 글자만으로는 안 보인다. 화살표는 장식이라
            // 읽어 주는 쪽에는 이 라벨이 대신 간다.
            aria-label="あそびかた（別のタブで開きます）"
            // 새 탭에서 이 화면을 열면 어느 탭에도 불이 안 들어온다. 그 자리를 여기가 맡는다.
            data-active={onGuide || undefined}
          >
            <span className="app-help__mark" aria-hidden="true">
              ?
            </span>
            あそびかた
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
                  {/* **대국 탭에만, 판을 두는 중에, 그 화면을 벗어나 있을 때만.** 판 위에
                      있으면 판이 보이므로 표식이 할 말이 없다. 점 하나인 것은 탭 폭을
                      늘리지 않기 위해서이고(좁은 화면에서 글자마다 접힌다), 무엇인지는
                      읽어 주는 쪽에 아래 글자로 나간다. */}
                  {tab.route.name === 'game' && playing && !onGame && (
                    <>
                      <span className="app-tab__live" aria-hidden="true" />
                      <span className="sr-only">対局中</span>
                    </>
                  )}
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
            ) : route.name === 'room' ? (
              // **방마다 새로 세운다.** `roomId` 만 갈아 끼우면 이 컴포넌트가 살아남아
              // 앞 방의 스냅샷과 시계를 한 틱 동안 그린다(퀴즈 화면과 같은 자리).
              <MatchScreen key={route.id} roomId={route.id} />
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
