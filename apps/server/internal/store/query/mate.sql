-- 詰み 캐시. positions 와 같은 이유로 유저·대국에 매이지 않는다 — 같은 국면은 누가
-- 두든 같은 답이고, 詰みまでの手数는 국면의 성질이라 거기까지 온 순서와 무관하다.

-- name: GetMate :one
SELECT * FROM mate_positions WHERE sfen_key = $1;

-- name: UpsertMate :one
--
-- 이 WHERE 절이 「얕은 한계의 답이 깊은 한계의 답을 덮지 않는다」를 지킨다. 없으면
-- 한계 9의 「없다」가 한계 11의 詰み을 지우고, 그 위에서 종반 판정이 돈다.
--
-- 같은 한계면 갱신하지 않는다. solver 는 같은 한계에서 결정적이라 답이 같고,
-- 다시 쓰면 created_at 이 늘 지금이 되어 언제 쌓인 것인지를 못 읽는다.
INSERT INTO mate_positions (sfen_key, depth_limit, moves)
VALUES ($1, $2, $3)
ON CONFLICT (sfen_key) DO UPDATE
SET depth_limit = EXCLUDED.depth_limit,
    moves       = EXCLUDED.moves
WHERE mate_positions.depth_limit < EXCLUDED.depth_limit
RETURNING *;

-- name: CountMatePositions :one
SELECT count(*) FROM mate_positions;
