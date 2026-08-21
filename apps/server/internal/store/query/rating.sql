-- 매칭 레이팅. 근거는 journal §92.
--
-- 밖으로 안 나간다. 어느 API 도 이 값을 돌려주지 않으므로 소유자 조건이 없다 —
-- 부르는 쪽이 대국 기록에서 얻은 두 user_id 로만 부른다(server/match_records.go).

-- name: GetRating :one
--
-- 없으면 0행이다. rating_games = 0 이면 행은 있어도 레이팅이 없는 것이고, 그 둘을
-- 부르는 쪽이 같게 다룬다 — 낙폭이 한 번이라도 저장된 사람은 그것 때문에 행이 이미 있다.
SELECT rating_est, rating_sd, rating_games, rating_updated_at, skill_loss, skill_samples
FROM skill_profile
WHERE user_id = $1;

-- name: SaveMatchRatings :exec
--
-- 한 문장으로 두 사람을 같이 옮긴다. 트랜잭션을 안 여는 이유는 여기가 원자적이면
-- 열 것이 없기 때문이다 — 반쪽만 반영된 판이 남으면 그 뒤로 두 사람의 레이팅이
-- 서로 다른 판 수 위에서 돈다.
--
-- 같은 user_id 를 두 번 넘기면 Postgres 가 거절한다(ON CONFLICT 가 한 행을 두 번 못
-- 고친다). 한 판의 두 사람은 언제나 다르므로 그 자리는 안 온다(match.Hub.Enter).
INSERT INTO skill_profile (user_id, rating_est, rating_sd, rating_games, rating_updated_at)
VALUES ($1, $2, $3, 1, now()),
       ($4, $5, $6, 1, now())
ON CONFLICT (user_id) DO UPDATE
SET rating_est        = EXCLUDED.rating_est,
    rating_sd         = EXCLUDED.rating_sd,
    -- 넘겨받은 값이 아니라 지금 값에 더한다. 부르는 쪽이 읽은 뒤로 다른 판이 끝났으면
    -- 그 판까지 세어야 하고, 세는 것이 이 칸의 유일한 일이다.
    rating_games      = skill_profile.rating_games + 1,
    rating_updated_at = now();
