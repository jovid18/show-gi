import { SIGN_IN_PATH, type MeResponse } from '@/protocol/auth';

/**
 * 헤더 오른쪽 끝의 로그인 자리.
 *
 * **로그인은 대국의 전제가 아니다.** 여기가 무엇을 그리든 판은 그대로 둘 수 있고,
 * 바뀌는 것은 그 판이 누구의 것으로 남느냐 하나다 — 그래서 안내 문구도, 막는 것도 없다.
 */
export function Account({ me, onSignOut }: { me: MeResponse; onSignOut: () => void }) {
  // 로그인이 없는 배포에서는 자리 자체가 없다. 눌러도 안 되는 버튼을 띄우면 고장으로 읽힌다.
  if (!me.enabled) return null;

  if (!me.user) {
    // **버튼이 아니라 링크다.** 브라우저를 통째로 Google로 보내는 이동이라
    // fetch로는 못 하고, 링크면 그 이동이 브라우저의 것이 된다.
    return (
      <a className="app-tab app-account" href={SIGN_IN_PATH}>
        ログイン
      </a>
    );
  }

  return (
    <div className="app-account">
      <span className="app-account-name" title={me.user.name}>
        {me.user.name}
      </span>
      <button className="app-tab" type="button" onClick={onSignOut}>
        ログアウト
      </button>
    </div>
  );
}
