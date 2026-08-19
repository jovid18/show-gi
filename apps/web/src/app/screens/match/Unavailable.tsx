import { navigate } from '@/routes/router';

/**
 * 열 수 없는 방.
 *
 * 왜인지를 안 갈라 말한다. 서버가 없는 방·만료된 방·남이 이미 찬 방·로그인 안 한
 * 요청을 전부 같은 404로 답하기 때문이고(방 id 를 훑어볼 수 없게), 화면이 여기서 갈라
 * 말하면 서버가 감춘 것이 그 자리에서 새어 나간다.
 *
 * 대신 가능한 이유를 전부 늘어놓는다. 하나만 적으면 다른 경우에 그것이 거짓이 된다.
 */
export function Unavailable() {
  return (
    <div className="setup">
      <h2 className="setup__head">この対局部屋は開けません</h2>
      <p className="setup__caveat">
        リンクが違うか、期限が切れたか、すでに二人が入っています。対局部屋は誰も入らないまま30分たつと閉じます。ログインしていない場合も同じ画面になります。
      </p>
      <button type="button" className="btn btn--primary setup__start" onClick={() => navigate({ name: 'game' })}>
        対局画面にもどる
      </button>
    </div>
  );
}
