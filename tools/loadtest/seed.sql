-- 부하 회차용 사용자. 실행은 사람이 DB 클라이언트로 직접 한다 —
-- 마이그레이션과 같은 규약이다(deploy/README.md §4).
--
-- provider 가 'loadtest' 다. users 에 UNIQUE (provider, provider_uid) 가 있으므로
-- 실사용자와 절대 안 섞이고, ON DELETE CASCADE 라 정리가 cleanup.sql 한 줄이다.
--
-- :n 은 만들 사람 수다. psql -v n=20 으로 넘긴다.
--
-- 레이팅을 심는 이유는 대기열의 밴드를 실측하려는 것이다. rating_games 를 1 이상으로
-- 두어야 그 값이 쓰인다 — 0은 「레이팅이 없다」이고 그때는 낙폭 추정치에서 시드를
-- 만든다(rating.SeedFromLoss).

BEGIN;

INSERT INTO users (provider, provider_uid, display_name)
SELECT 'loadtest', 'lt-' || i, 'LT' || i
FROM generate_series(1, :n) AS s(i)
ON CONFLICT (provider, provider_uid) DO NOTHING;

-- 레이팅을 1200 부터 100 씩 벌린다. 밴드가 대기 시간으로 넓어지는 것을 보려면 짝이
-- 바로 안 잡히는 간격이 필요하다 — 기본 밴드가 좁은 쪽에서 시작하기 때문이다.
INSERT INTO skill_profile (user_id, rating_est, rating_sd, rating_games, rating_updated_at)
SELECT u.id,
       1200 + 100 * (row_number() OVER (ORDER BY u.id) - 1),
       80,
       10,
       now()
FROM users u
WHERE u.provider = 'loadtest'
ON CONFLICT (user_id) DO UPDATE
    SET rating_est        = EXCLUDED.rating_est,
        rating_sd         = EXCLUDED.rating_sd,
        rating_games      = EXCLUDED.rating_games,
        rating_updated_at = EXCLUDED.rating_updated_at;

COMMIT;

-- 도구가 읽을 목록. 쿠키를 굽는 데 id 와 이름이 필요하다.
SELECT id, display_name FROM users WHERE provider = 'loadtest' ORDER BY id;
