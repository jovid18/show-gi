import { useEffect, useState } from 'react';

type Health = 'checking' | 'up' | 'down';

/**
 * 아직 화면이 없다. 지금 이 컴포넌트가 확인하는 것은 하나다 —
 * 브라우저에서 Go 서버까지 경로가 뚫려 있는가. 개발에서는 Vite 프록시가,
 * 배포에서는 리버스 프록시가 같은 상대 경로를 서버로 넘긴다.
 *
 * 화면에 나가는 문자열은 전부 일본어다. 사용자는 일본인이고, 한글이 하나라도
 * 섞이면 그 자리에서 "번역이 덜 된 앱"이 된다.
 */
export function App() {
  const [health, setHealth] = useState<Health>('checking');

  useEffect(() => {
    const controller = new AbortController();

    fetch('/healthz', { signal: controller.signal })
      .then((res) => setHealth(res.ok ? 'up' : 'down'))
      .catch(() => {
        // 언마운트로 취소된 요청까지 'down'으로 칠하면 개발 중 StrictMode에서
        // 멀쩡한 서버가 죽은 것처럼 보인다.
        if (!controller.signal.aborted) setHealth('down');
      });

    return () => controller.abort();
  }, []);

  return (
    <main>
      <h1>show-gi</h1>
      <p className="tagline">口を出すときを自分で決める将棋の相手</p>
      <p className={`health health--${health}`}>
        {health === 'checking' && 'API を確認しています…'}
        {health === 'up' && 'API に接続しました'}
        {health === 'down' && 'API に接続できません'}
      </p>
    </main>
  );
}
