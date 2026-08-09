-- 국면 캐시. 같은 국면은 누가 두든 같은 결과이므로 유저·대국에 매이지 않는다.
--
-- 키는 手数를 뺀 SFEN이라 전치(transposition)가 한 행으로 합쳐진다.
-- 자세한 것은 docs/02-architecture.md §4.

-- name: GetPosition :one
SELECT * FROM positions WHERE sfen_key = $1;

-- name: UpsertPosition :one
--
-- **더 얕게 계산한 결과가 깊은 결과를 덮지 않는다.** 이 WHERE 절이 그 규칙 전부다.
-- 없으면 depth 10 계산이 depth 14 결과를 지우고, 개입 판정이 얕은 값 위에서 돈다.
--
-- 같은 깊이면 덮어쓰지 않는다 — 같은 국면·같은 깊이는 같은 결과라서(엔진을 1스레드
-- 고정 깊이로 돌리는 이유가 이것이다) 쓸 이유가 없다.
INSERT INTO positions (sfen_key, side_to_move, ply_hint, candidates, computed_depth)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (sfen_key) DO UPDATE
SET candidates     = EXCLUDED.candidates,
    ply_hint       = EXCLUDED.ply_hint,
    computed_depth = EXCLUDED.computed_depth
WHERE positions.computed_depth < EXCLUDED.computed_depth
RETURNING *;

-- name: CountPositions :one
SELECT count(*) FROM positions;
