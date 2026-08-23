// 세션 쿠키를 굽는다. 로그인 경로를 지나지 않는다.
//
// 세션은 서버에 짝이 없는 서명 쿠키 하나다(internal/auth 의 Codec) — 서버가 하는 일이
// HMAC 검증뿐이라, 비밀을 아는 쪽은 Google 왕복 없이 같은 값을 만들 수 있다.
// 부하 도구에 로컬 로그인이 필요 없는 이유가 이것이다(journal §103).
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';

// mintSession 은 그 사용자의 쿠키 값을 만든다. ttl 은 초다.
export function mintSession(secret, userID, name, ttlSeconds) {
  const exp = Math.floor(Date.now() / 1000) + ttlSeconds;
  const payload = encoding.b64encode(JSON.stringify({ uid: userID, name, exp }), 'rawurl');
  // 서명은 base64 raw url 이다. Codec.sign 과 같은 인코딩이라야 검증을 통과한다.
  const sig = crypto.hmac('sha256', secret, payload, 'base64rawurl');
  return `${payload}.${sig}`;
}

// cookieHeader 는 요청에 실을 헤더다. 쿠키 이름은 서버가 정한 것이다.
export function cookieHeader(value) {
  return { Cookie: `showgi_session=${value}` };
}
