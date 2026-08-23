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

-- 레이팅을 1100 에서 700 씩 벌린다. 밴드가 대기로 넓어지는 것을 보려면 짝이 바로 안
-- 잡히는 간격이 필요하다.
--
-- 700 은 계산으로 나온 값이다. 큐는 아무 둘이나 밴드 안에 있으면 붙이므로(internal/queue
-- 의 Pairable) 기준은 심은 집합의 최소 쌍거리다. 밴드가 Base0(200) + Expand(20)·기다린초
-- + 두 사람의 rating_sd 이니, 아래 sd 80 에서 처음 섰을 때가 360 이고 상한이 960 이다.
--
--   360 < 최소 쌍거리 <= 960
--
-- 아래쪽을 어기면 전부 즉시 붙어 넓어지는 것을 못 보고, 위쪽을 어기면 아무도 안 붙는다.
-- 700 이면 이웃끼리 (700-360)/20 = 17초에 붙는다. sd 를 바꾸면 다시 계산한다.
--
-- 밴드를 재는 회차는 n=2 다. 이 척도가 1500 중심에 시드가 ±400 이라(internal/rating 의
-- Default·SeedSpread) 실사용자는 1100~1900 폭 800 에 있고, 그 안에 최소 쌍거리 360 을
-- 지키는 사람은 둘까지다 — 셋이면 400 간격이라 2초에 붙고, 넷은 3*360 이 800 을 넘어
-- 아예 안 된다. n 을 더 키우면 레이팅이 척도를 벗어난다(n=12 면 끝이 8900 이다).
--
-- 엔진 회차는 레이팅을 안 보므로 n 을 크게 잡아도 된다. 큰 값이 새지도 않는다 —
-- skill_profile 을 읽는 질의가 둘뿐이고(GetRating·GetSkill) 둘 다 user_id 하나를 보며,
-- 段級 앵커는 집계가 아니라 상수다(internal/skill 의 AnchorFromPly).
INSERT INTO skill_profile (user_id, rating_est, rating_sd, rating_games, rating_updated_at)
SELECT u.id,
       1100 + 700 * (row_number() OVER (ORDER BY u.id) - 1),
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
