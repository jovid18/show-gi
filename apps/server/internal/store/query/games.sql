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
--
-- `explain_tier` 는 **LLM을 안 거쳤으면 NULL** 이다. 0으로 적으면 「캐시 히트」와 구별이
-- 안 되는데, 그 둘은 비용 계측에서 정반대의 뜻이다 — 히트는 아껴서 0엔이고 NULL은 애초에
-- 부르지 않은 것이다(docs/04-llm.md §2).
-- `best_cp`·`after_cp` 는 낙폭을 만든 **두 원본**이다(둘 다 수번 측 관점). 낙폭만 남기면
-- 되돌릴 수 없어서 재채점도 절대값 비교도 못 한다 — 판정이 이미 손에 들고 있는 값이라
-- 남기는 데 드는 것이 없다(migrations/005). **과거 행은 NULL 이다.**
INSERT INTO interventions (
    game_id, ply, kind, category, delta_win, level_bucket, retracted_usi,
    explain_tier, cost_yen, best_cp, after_cp
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

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

-- ─── 리뷰(읽기) ─────────────────────────────────────────────
-- 여기부터가 **꺼내는 쪽**이다. 위의 질의들이 쌓기만 하고 아무도 안 읽던 자리다.

-- name: ListGames :many
--
-- 리뷰 화면의 첫 목록. 최신부터.
--
-- **한 수도 안 둔 판은 빼고 센다.** 연결만 열렸다 끊긴 판이 실제로 그렇게 남는데
-- (ws 핸들러가 붙는 즉시 CreateGame 한다), 되짚을 것이 없는 줄을 목록 맨 위에 놓으면
-- 진짜 대국이 아래로 밀린다. 세는 쪽과 거르는 쪽이 같은 EXISTS 라 둘이 어긋나지 않는다.
--
-- 정렬은 id 하나로 한다. started_at 은 now() 라 같은 초에 여러 판이 들어가면 순서가
-- 흔들리는데, id 는 시퀀스라 그 자리에서 갈린다.
SELECT
    g.id,
    g.my_color,
    g.started_at,
    g.finished_at,
    g.result,
    (SELECT count(*) FROM game_moves m WHERE m.game_id = g.id) AS move_count,
    (SELECT count(*) FROM interventions i WHERE i.game_id = g.id) AS intervention_count
FROM games g
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
ORDER BY g.id DESC
LIMIT $1;

-- name: GetGame :one
--
-- **여기서는 개입을 세지 않는다.** 어차피 아래에서 전부 읽어 오므로, 따로 센 숫자와
-- 실제로 온 줄 수가 두는 중인 판에서 어긋날 수 있다 — 목록(ListGames)은 줄을 안 읽으니
-- 거기서만 센다.
SELECT id, my_color, started_at, finished_at, result, start_sfen
FROM games
WHERE id = $1;

-- name: ListGameMoves :many
--
-- eval_cp 는 **先手 관점**이고 NULL일 수 있다 — 평가치는 수보다 늦게 오므로,
-- 연결이 끊긴 판의 마지막 몇 수에는 안 채워진 채로 남는다.
SELECT ply, usi, eval_cp FROM game_moves WHERE game_id = $1 ORDER BY ply;

-- name: ListGameInterventions :many
--
-- 같은 ply에 여러 행이 온다(InsertIntervention 참조). id 로 이어 정렬해 **물러진
-- 순서**를 지킨다 — 한 국면에서 두 번 걸렸을 때 어느 쪽이 먼저였는지가 곧 이야기다.
SELECT ply, kind, category, delta_win, level_bucket, retracted_usi, best_cp, after_cp
FROM interventions
WHERE game_id = $1
ORDER BY ply, id;
