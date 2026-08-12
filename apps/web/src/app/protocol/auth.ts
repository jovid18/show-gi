// `/api/me`의 계약. 서버의 `internal/server/auth.go`와 짝이다.

export interface Viewer {
  id: number;
  name: string;
}

export interface MeResponse {
  /**
   * 이 배포에 로그인이라는 것이 있는가.
   *
   * **`user === null`과 다른 말이다.** 하나로 합치면 키가 없는 환경에서 눌러도
   * 아무 일도 안 일어나는 버튼이 뜨고, 그건 사람에게 고장으로 읽힌다.
   */
  enabled: boolean;
  user: Viewer | null;
}

/** 로그인을 시작하는 곳. 브라우저를 통째로 보낸다 — fetch로는 Google 화면을 못 띄운다. */
export const SIGN_IN_PATH = '/api/auth/google/start';
