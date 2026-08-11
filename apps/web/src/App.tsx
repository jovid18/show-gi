import { useState } from 'react';

import { GameScreen } from './components/GameScreen';
import { ReviewScreen } from './components/ReviewScreen';

type Screen = 'game' | 'review';

const TAB_JA: Record<Screen, string> = {
  game: '対局',
  review: '振り返り',
};

/**
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  const [screen, setScreen] = useState<Screen>('game');

  return (
    <div className="app">
      <header className="app-head">
        <h1 className="app-title">show-gi</h1>
        <p className="app-tagline">口を出すときを自分で決める将棋の相手</p>

        <nav className="app-tabs" aria-label="画面">
          {(['game', 'review'] as const).map((tab) => (
            <button
              key={tab}
              type="button"
              className="app-tab"
              data-active={screen === tab || undefined}
              aria-current={screen === tab ? 'page' : undefined}
              onClick={() => setScreen(tab)}
            >
              {TAB_JA[tab]}
            </button>
          ))}
        </nav>
      </header>

      {/*
        **대국 화면은 감추기만 한다.** 대국은 WebSocket 연결 하나에 매여 있어서, 여기서
        컴포넌트를 내리면 연결이 끊기고 그 판은 `abandoned` 로 닫힌다 — 두던 판을 두고
        지난 판을 보러 갔다가 돌아오면 판이 사라져 있는 것이 된다.
      */}
      <div hidden={screen !== 'game'}>
        <GameScreen />
      </div>

      {/* 리뷰는 열 때마다 새로 부른다. 방금 끝난 판이 목록 맨 위에 있어야 한다. */}
      {screen === 'review' && <ReviewScreen />}
    </div>
  );
}
