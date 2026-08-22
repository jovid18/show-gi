-- 검토 화면에서 저장한 국면(migrations/015). 판이 아니라 手合割 id 와 수순 한 줄이다.

-- name: CreateExploreSnapshot :one
--
-- 개수를 안 막는다. 근거는 journal §96.
INSERT INTO explore_snapshots (user_id, name, handicap, moves)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at;

-- name: ListExploreSnapshots :many
--
-- 주인을 = 로 받는다. 익명(user_id NULL)이 애초에 안 걸린다.
--
-- LIMIT 이 없다. 개수 상한이 없으므로 여기서 자르면 지울 수 없는 행이 생긴다(journal §96).
SELECT id, name, handicap, moves, created_at
FROM explore_snapshots
WHERE user_id = $1
ORDER BY id DESC;

-- name: RenameExploreSnapshot :execrows
--
-- 주인이 아니면 0행이고, 부르는 쪽에서 그것이 404가 된다(GetGameForOwner 와 같은 규약).
UPDATE explore_snapshots
SET name = $3
WHERE id = $1
  AND user_id = $2;

-- name: DeleteExploreSnapshot :execrows
--
-- 위와 같은 이유로 주인을 조건에 둔다.
DELETE FROM explore_snapshots
WHERE id = $1
  AND user_id = $2;
