-- 詰め将棋 풀이의 캐시와 사람마다의 기록(migrations/025).

-- name: GetTsumeResult :one
SELECT * FROM tsume_results WHERE sfen_key = $1 AND solver = $2;

-- name: InsertTsumeResult :exec
--
-- 이미 있으면 덮지 않는다. 같은 판의 풀이는 같은 답을 내고, 다시 쓰면 created_at 이 늘
-- 지금이 된다(UpsertMate 와 같은 판단).
INSERT INTO tsume_results (sfen_key, solver, status, plies, shortest, tree, nodes)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (sfen_key, solver) DO NOTHING;

-- name: UpsertTsumeSolve :exec
--
-- 같은 국면을 다시 풀면 답과 시각만 고친다. 사람이 붙인 이름은 그대로 둔다.
INSERT INTO tsume_solves (user_id, sfen_key, status, plies, shortest)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, sfen_key) DO UPDATE
SET status    = EXCLUDED.status,
    plies     = EXCLUDED.plies,
    shortest  = EXCLUDED.shortest,
    solved_at = now();

-- name: ListTsumeSolves :many
--
-- LIMIT 이 없다. 개수 상한이 없으므로 여기서 자르면 지울 수 없는 행이 생긴다
-- (ListExploreSnapshots 와 같은 판단).
SELECT id, sfen_key, name, status, plies, shortest, solved_at
FROM tsume_solves
WHERE user_id = $1
ORDER BY solved_at DESC, id DESC;

-- name: RenameTsumeSolve :execrows
--
-- 주인이 아니면 0행이고, 부르는 쪽에서 그것이 404가 된다.
UPDATE tsume_solves
SET name = $3
WHERE id = $1
  AND user_id = $2;

-- name: DeleteTsumeSolve :execrows
DELETE FROM tsume_solves
WHERE id = $1
  AND user_id = $2;
