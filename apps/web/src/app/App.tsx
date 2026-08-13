import { lazy, Suspense } from 'react';

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
 * `needsAuth` 는 **로그인이 있는 배포에서만** 그리는 탭이다.
 *
 * 마이페이지는 익명에게 401이라(profile.go) 로그인 없는 배포에서는 갈 곳이 없다 —
 * 「눌러도 안 되는 버튼을 띄우면 고장으로 읽힌다」가 `Account` 에 이미 있는 규칙이고,
 * 탭이라고 다르지 않다.
 */
const TABS: { route: Route; label: string; needsAuth?: boolean }[] = [
  { route: { name: 'game' }, label: '対局' },
  { route: { name: 'reviews' }, label: '振り返り' },
  { route: { name: 'me' }, label: 'マイページ', needsAuth: true },
];

/**
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  const route = useRoute();
  const onGame = route.name === 'game';
  const onMe = route.name === 'me';
  const { me, signOut } = useViewer();

  return (
    <>
      {/* 화면 전폭을 쓰고 스크롤에 붙어 있는다. **겹은 장막(40)보다 낮다** — 개입이
          걸리면 이 줄도 방과 함께 어두워져야 하고, 위에 있으면 판만 남는 그림이 깨진다. */}
      <header className="app-head">
        <div className="app-head__inner">
          <a className="app-brand" href={hrefOf({ name: 'game' })}>
            {/* 판의 駒와 같은 明朝다. 로고를 따로 그리지 않고 게임 자신의 글자를 쓴다. */}
            <span className="app-mark" aria-hidden="true">
              将
            </span>
            <span className="app-brand__text">
              <span className="app-title">show-gi</span>
              <span className="app-tagline">口を出すときを自分で決める将棋の相手</span>
            </span>
          </a>

          <nav className="app-tabs" aria-label="画面">
            {TABS.filter((tab) => !tab.needsAuth || me.enabled).map((tab) => {
              // **버튼이 아니라 링크다.** 주소가 화면을 정하므로 가운데 클릭·링크 복사·
              // 새 탭이 그냥 동작해야 하고, 그건 `<a href>` 만이 준다.
              // 세 갈래라 「대국이냐 아니냐」로는 못 가른다 — `reviews` 탭이 켜지는
              // 조건이 되짚기 셋(목록·상세·퀴즈)이고 마이페이지는 거기서 빠진다.
              const active = tab.route.name === 'game' ? onGame : tab.route.name === 'me' ? onMe : !onGame && !onMe;
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
            {onMe ? (
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
