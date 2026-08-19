-- 되짚기 퀴즈. 한 판에 한 행이고 늘 통째로 읽고 쓴다(migrations/007).

-- name: GetGameQuiz :one
SELECT version, payload FROM game_quizzes WHERE game_id = $1;

-- name: UpsertGameQuiz :exec
--
-- 덮어쓰는 조건을 안 건다. positions 와 다른 점이고(저쪽은 얕은 결과가 깊은 것을 덮지
-- 못하게 막는다), 이쪽은 한 판에 생성이 한 번이라 경합할 상대가 없다. 다시 만드는
-- 자리가 생기면 그때 새 결과가 정본이다.
INSERT INTO game_quizzes (game_id, version, payload)
VALUES ($1, $2, $3)
ON CONFLICT (game_id) DO UPDATE
SET version = EXCLUDED.version, payload = EXCLUDED.payload, generated_at = now();
