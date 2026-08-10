-- 대국 기록. 기보와 개입을 남긴다.
--
-- **개입으로 물러진 수가 여기서만 남는다.** 기보(game_moves)에는 확정된 수만 들어가므로,
-- interventions 에 안 적으면 「개입이 막지 않았다면 실제로 뒀을 수」가 그대로 사라진다 —
-- 개입에 오염되지 않은 유일한 실력 신호다 (docs/01-core.md §5).

-- name: CreateGame :one
--
-- user_id 는 로그인 전이면 NULL이다 (002_anonymous_games.sql).
INSERT INTO games (user_id, my_color, start_sfen)
VALUES ($1, $2, $3)
RETURNING id;

-- name: FinishGame :exec
UPDATE games SET finished_at = now(), result = $2 WHERE id = $1;

-- name: InsertMove :exec
--
-- **확정된 수만 들어온다.** 물러진 수가 여기 들어가면 기보가 롤백을 반영하지 못한다.
--
-- 같은 ply를 다시 쓰는 것은 롤백 뒤 다시 둔 경우다. 덮어쓴다 — 기보는 「지금 판에
-- 남아 있는 수순」이지 시도의 목록이 아니다. 시도는 interventions 가 센다.
INSERT INTO game_moves (game_id, ply, usi, eval_cp)
VALUES ($1, $2, $3, $4)
ON CONFLICT (game_id, ply) DO UPDATE
SET usi = EXCLUDED.usi, eval_cp = EXCLUDED.eval_cp;

-- name: InsertIntervention :exec
--
-- **같은 ply에 여러 행이 들어간다.** 한 국면에서 몇 수를 시도하고 전부 물러지는 일이
-- 실제로 있고(docs/06-status.md §17), 그 반복 자체가 기록할 값이다. 그래서
-- (game_id, ply) 는 유니크가 아니다.
INSERT INTO interventions (game_id, ply, kind, category, delta_win, level_bucket, retracted_usi)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: CountGames :one
SELECT count(*) FROM games;

-- name: CountInterventions :one
SELECT count(*) FROM interventions;

-- name: SetMoveEval :exec
--
-- 평가치만 나중에 채운다. **수를 덮지 않는다** — 그 수는 이미 확정되어 들어가 있고,
-- 여기서 다시 쓰면 물러진 수로 덮을 길이 생긴다.
--
-- 없는 ply면 아무 일도 안 한다. 평가치가 수보다 먼저 오는 경로는 없으므로 그때는
-- 기록이 실패한 것이고, 그걸 여기서 만들어 메우면 기보에 없는 행이 생긴다.
UPDATE game_moves SET eval_cp = $3 WHERE game_id = $1 AND ply = $2;
