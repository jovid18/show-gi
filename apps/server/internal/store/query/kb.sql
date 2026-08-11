-- name: KnowledgeForTags :many
-- 태그로 kb_chunks를 찾는다. GIN 인덱스가 잡는다(001_init.sql).
-- verified_by IS NOT NULL 조건은 부분 인덱스와 같은 절이다 — 검증 안 된 행은 안 나온다.
SELECT title, body
FROM kb_chunks
WHERE tags && @tags::text[]
  AND verified_by IS NOT NULL;
