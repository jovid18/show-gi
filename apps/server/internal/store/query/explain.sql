-- name: CachedExplanation :one
--
-- 키에 걸린 문장을 주면서 **히트를 센다.**
--
-- `UPDATE ... RETURNING` 이라 왕복이 한 번이다. SELECT 뒤에 UPDATE를 따로 두면 개입 판정
-- 경로에 왕복이 둘이 되고, 무엇보다 두 문장 사이에서 세는 것을 빠뜨리기 쉽다 — `hits` 는
-- 발표에 나가는 캐시 히트율의 분자다(docs/04-llm.md §5).
--
-- 없는 키면 아무 행도 안 돌아온다. 부르는 쪽이 그것을 「miss」로 읽는다.
UPDATE explain_cache
SET hits = hits + 1
WHERE key = $1
RETURNING body;

-- name: SaveExplanation :exec
--
-- 만든 문장을 남긴다. **같은 키가 있으면 덮지 않는다.**
--
-- 먼저 만들어진 문장은 이미 화면에 나갔다. 같은 사실에 다른 문장이 나오기 시작하면
-- 「같은 실수에는 같은 설명」이 깨지고, 문구를 고쳤을 때 무엇이 달라졌는지도 못 본다.
-- 프롬프트를 고치는 쪽은 키를 바꾼다(explain.promptVersion).
INSERT INTO explain_cache (key, body, model)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO NOTHING;

-- name: ExplainCacheStats :one
--
-- 캐시가 실제로 듣고 있는지 보는 값이다. 발표 슬라이드의 히트율이 여기서 나온다.
SELECT count(*) AS entries, coalesce(sum(hits), 0)::bigint AS hits
FROM explain_cache;
