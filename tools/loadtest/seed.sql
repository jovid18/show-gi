-- 부하 회차용 사용자. 실행은 사람이 DB 클라이언트로 직접 한다 —
-- 마이그레이션과 같은 규약이다(deploy/README.md §4).
--
-- provider 가 'loadtest' 다. users 에 UNIQUE (provider, provider_uid) 가 있으므로
-- 실사용자와 절대 안 섞이고, ON DELETE CASCADE 라 정리가 cleanup.sql 한 줄이다.
--
-- **psql 전용 문법을 쓰지 않는다.** DataGrip 은 :n 을 자기 파라미터로 잡고 \if 를
-- 문장으로 읽어서, 한 번은 n=16 이 spread 로 들어가 사용자가 안 늘었다(journal §108).
-- 프로덕션 DB 는 사람이 DataGrip 으로 도는 자리라 그쪽이 기준이다.
--
-- 고칠 값은 둘이고 각각 한 군데에만 있다 — 아래 INSERT 두 개의 주석이 가리킨다.

BEGIN;

-- ① 만들 사람 수. 아래 generate_series 의 두 번째 인자다.
--
-- **VU 합보다 많아야 한다**(README 의 LT_UIDS). 대인전은 2 VU 가 판 하나이므로
-- 「동시 판수 × 2 + 여유」이고, 64 면 31판까지 걸 수 있다. 2대의 천장이 12판이라
-- (journal §122) 3대·4대를 재는 계단까지 이 값으로 덮인다.
--
-- 늘리기만 하면 된다. ON CONFLICT DO NOTHING 이라 이미 있는 사람은 그대로 남는다 —
-- **다만 cleanup.sql 을 돌린 뒤라면 번호가 전부 새로 붙으므로 아래 SELECT 를 다시 읽는다.**
INSERT INTO users (provider, provider_uid, display_name)
SELECT 'loadtest', 'lt-' || i, 'LT' || i
FROM generate_series(1, 64) AS s(i)
ON CONFLICT (provider, provider_uid) DO NOTHING;

-- 레이팅. 기본은 전원이 척도의 중앙(internal/rating 의 Default)이다.
--
-- spread 가 0이면 전부 1500 이고 짝이 즉시 잡힌다 — 「대인전이 도는가」를 보는
-- 회차가 그것을 원한다. 벌리면 그 폭으로 중앙을 기준삼아 늘어선다.
--
-- rating_games 를 1 이상으로 두어야 그 값이 쓰인다 — 0은 「레이팅이 없다」이고
-- 그때는 낙폭 추정치에서 시드를 만든다(rating.SeedFromLoss).
--
-- 밴드가 대기로 넓어지는 것을 보려면 벌려야 하고, 그 회차는 사람이 둘이다. 이유가
-- 두 조건이 겹쳐서다. 대기열은 아무 둘이나 밴드 안에 있으면 붙이므로(internal/queue 의
-- Pairable) 기준은 최소 쌍거리인데,
--
--   최소 쌍거리 > 360          처음 섰을 때의 밴드(Base0 200 + sd 80 + sd 80)
--   (n-1) * spread <= 800      시드가 Default ± SeedSpread 안에 있어야 한다
--
-- 둘을 같이 만족하는 n 은 2뿐이다 — n=3 이면 간격이 400 이하라 2초에 붙어서 안 보인다.
-- n=2, spread=700 이면 1150·1850 이고 (700-360)/20 = 17초에 붙는다. sd 를 바꾸면 위
-- 360 을 다시 계산한다.
--
-- 이 값을 척도 밖으로 내보내지 않는 것이 규칙이다. 지금은 새는 곳이 없지만(skill_profile 을
-- 읽는 질의가 둘뿐이고 둘 다 user_id 하나를 본다) 그건 조용히 바뀔 수 있는 성질이고,
-- 8000점짜리 행은 순위표나 段級 분포를 붙이는 날 오염원이 된다.

-- ② 레이팅 폭. 0 이면 전원 1500 이다. 밴드 회차만 700 으로 고친다.
INSERT INTO skill_profile (user_id, rating_est, rating_sd, rating_games, rating_updated_at)
SELECT u.id,
       1500 + 0 * (row_number() OVER (ORDER BY u.id) - 1
           - (count(*) OVER () - 1) / 2.0),
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
