-- 탐색 하나의 소요 시간. depth·k 별 비용을 나중에 보기 위한 기록이다.
-- 서버는 쓰기만 하고 읽는 쪽은 사람이다(DB 클라이언트).

-- name: InsertSearchTiming :exec
INSERT INTO search_timings (sfen_key, depth, k, ms, cached)
VALUES ($1, $2, $3, $4, $5);
