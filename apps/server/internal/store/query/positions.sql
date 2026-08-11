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
-- **같은 깊이라도 후보가 더 많으면 덮는다.** 처음에는 「같은 국면·같은 깊이는 같은
-- 결과」라고 두었는데, 같은 깊이에서도 **MultiPV가 갈린다** — 상대 수는 k=10, 개입
-- 판정은 k=1, 가정 수순은 k=3으로 묻는다. 그러면 k=1로 먼저 쓰인 행이 k=3을 영구히
-- 막고, 그쪽은 캐시를 못 쓴 채 매번 다시 잰다. 같은 깊이에서 후보가 많은 것은 **더
-- 많이 아는 것**이라 남기는 쪽이 맞다.
INSERT INTO positions (sfen_key, side_to_move, ply_hint, candidates, computed_depth)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (sfen_key) DO UPDATE
SET candidates     = EXCLUDED.candidates,
    ply_hint       = EXCLUDED.ply_hint,
    computed_depth = EXCLUDED.computed_depth
WHERE positions.computed_depth < EXCLUDED.computed_depth
   OR (positions.computed_depth = EXCLUDED.computed_depth
       AND coalesce(jsonb_array_length(positions.candidates), 0)
         < coalesce(jsonb_array_length(EXCLUDED.candidates), 0))
RETURNING *;

-- name: CountPositions :one
SELECT count(*) FROM positions;

-- 국면 사이의 한 수. **분석을 버리지 않기 위한 자리다** — 깊이별 평가치와 그 수가 만든
-- 이름(囲い·전법·手筋)이 여기 남는다. 자세한 것은 docs/02-architecture.md §4.

-- name: UpsertEdge :exec
--
-- **아는 것만 채우고 남의 칸을 지우지 않는다.** 한 수의 사실이 두 번에 걸쳐 온다 —
-- 후보를 잴 때는 깊이별 평가치를 알고, 그 수를 실제로 두어 자식 국면을 잴 때는
-- 도착 국면과 태그를 안다. 늦게 오는 쪽이 먼저 온 것을 지우면 절반이 사라진다.
INSERT INTO edges (parent_key, usi, child_key, tags, eval_by_depth)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (parent_key, usi) DO UPDATE
SET child_key     = COALESCE(EXCLUDED.child_key, edges.child_key),
    tags          = CASE WHEN cardinality(EXCLUDED.tags) > 0
                         THEN EXCLUDED.tags ELSE edges.tags END,
    eval_by_depth = CASE WHEN cardinality(EXCLUDED.eval_by_depth) > 0
                         THEN EXCLUDED.eval_by_depth ELSE edges.eval_by_depth END;

-- name: CountEdges :one
SELECT count(*) FROM edges;
