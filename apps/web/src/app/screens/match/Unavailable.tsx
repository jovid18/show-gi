import { useT } from '@/i18n';
import { navigate } from '@/routes/router';

/**
 * 열 수 없는 방.
 *
 * 왜인지를 구분해 말하지 않는다. 서버가 없는 방·만료된 방·남이 이미 찬 방·로그인하지 않은
 * 요청을 전부 같은 404로 답하므로(방 id 를 훑어볼 수 없게), 화면이 갈라 말하면 서버가 감춘
 * 것이 새어 나간다.
 *
 * 대신 가능한 이유를 전부 늘어놓는다. 하나만 적으면 다른 경우에 그것이 거짓이 된다.
 */
export function Unavailable() {
  const t = useT();
  return (
    <div className="setup">
      <h2 className="setup__head">{t.match.unavailable.head}</h2>
      <p className="setup__caveat">{t.match.unavailable.body}</p>
      <button type="button" className="btn btn--primary setup__start" onClick={() => navigate({ name: 'game' })}>
        {t.match.backToGame}
      </button>
    </div>
  );
}
