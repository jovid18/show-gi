import { GameScreen } from './components/GameScreen';

/**
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  return (
    <div className="app">
      <header className="app-head">
        <h1 className="app-title">show-gi</h1>
        <p className="app-tagline">口を出すときを自分で決める将棋の相手</p>
      </header>
      <GameScreen />
    </div>
  );
}
