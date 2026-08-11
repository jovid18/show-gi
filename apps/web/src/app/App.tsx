import { GameScreen } from '@/screens/game/GameScreen';
import { ReviewScreen } from '@/screens/review/ReviewScreen';
import { hrefOf, navigate, useRoute, type Route } from '@/routes/router';

const TABS: { route: Route; label: string }[] = [
  { route: { name: 'game' }, label: '対局' },
  { route: { name: 'reviews' }, label: '振り返り' },
];

/**
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  const route = useRoute();
  const onGame = route.name === 'game';

  return (
    <div className="app">
      <header className="app-head">
        <h1 className="app-title">show-gi</h1>
        <p className="app-tagline">口を出すときを自分で決める将棋の相手</p>

        <nav className="app-tabs" aria-label="画面">
          {TABS.map((tab) => {
            // **버튼이 아니라 링크다.** 주소가 화면을 정하므로 가운데 클릭·링크 복사·
            // 새 탭이 그냥 동작해야 하고, 그건 `<a href>` 만이 준다.
            const active = tab.route.name === 'game' ? onGame : !onGame;
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
      </header>

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
      {!onGame && <ReviewScreen route={route} />}
    </div>
  );
}
