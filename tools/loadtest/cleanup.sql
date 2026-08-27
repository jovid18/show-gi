-- 회차가 끝나면 돌린다. 한 줄이면 되는 이유는 users 로의 FK 가 전부
-- ON DELETE CASCADE 이기 때문이다 — 판·기보·평가치·실력 프로파일·레이팅·대기열 행이
-- 같이 사라진다.
--
-- 실사용자는 provider 가 'google' 이라 안 걸린다.
DELETE FROM users WHERE provider = 'loadtest';
