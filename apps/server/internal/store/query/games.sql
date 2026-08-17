-- 대국 기록. 기보와 개입을 남긴다.
--
-- **개입으로 물러진 수가 여기서만 남는다.** 기보(game_moves)에는 확정된 수만 들어가므로,
-- interventions 에 안 적으면 그 수가 그대로 사라진다(docs/01-core.md §5).

-- name: CreateGame :one
--
-- user_id 는 로그인 전이면 NULL이다 (002_anonymous_games.sql).
--
-- opening_tag 는 사람이 고른 **상대의** 진형 id다 (internal/book). 「おまかせ」면 NULL.
-- 이 칸이 있어야 이어하기가 상대를 원래대로 다시 세운다 — 북은 상태를 안 들고 매번
-- (start_sfen, moves) 에서 다시 구하므로(game.bookOpponent) id 하나면 그 자리로 돌아간다.
INSERT INTO games (user_id, my_color, start_sfen, opening_tag)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: CreateMatchGame :one
--
-- 대인전 한 판의 **한쪽 몫**이다. 같은 대국이 이 질의로 두 번 불려 행 두 개가 된다 —
-- 그래야 소유 검사를 타는 다섯 질의가 한 줄도 안 바뀐다(012_match_games.sql).
--
-- **`opening_tag` 가 없다.** 그 칸은 「사람이 고른 컴퓨터의 진형」이라 상대가 사람이면
-- 채울 것이 없고, 빈 값을 넣으면 이어하기가 없는 북을 찾는다.
--
-- **`user_id` 가 NULL 로 오지 않는다.** 대인전은 로그인한 사람만이라(internal/match 의 Room)
-- 익명 판이 여기로 올 수 없다 — 컬럼 자체는 그대로 nullable 이다(002).
INSERT INTO games (user_id, my_color, start_sfen, match_id)
VALUES ($1, $2, $3, $4)
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
-- **(game_id, ply) 는 유니크가 아니다.** 한 국면에서 몇 수를 시도하고 전부 물러지는 일이
-- 실제로 있고 그 반복이 곧 기록할 값이다(journal §17).
-- 칸별 규약은 store.Intervention 에 있다.
INSERT INTO interventions (
    game_id, ply, kind, category, delta_win, level_bucket, retracted_usi,
    best_cp, after_cp
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CountGames :one
SELECT count(*) FROM games;

-- name: CountInterventions :one
SELECT count(*) FROM interventions;

-- name: SetMoveEval :exec
--
-- 평가치만 채운다. **수를 덮지 않는다** — upsert로 두면 물러진 수로 기보를 덮는 길이 생긴다.
-- 없는 ply면 아무 일도 안 한다(평가치가 수보다 먼저 오는 경로가 없다).
UPDATE game_moves SET eval_cp = $3 WHERE game_id = $1 AND ply = $2;

-- ─── 리뷰(읽기) ─────────────────────────────────────────────

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
    (SELECT count(*) FROM interventions i WHERE i.game_id = g.id) AS intervention_count,
    g.match_id
FROM games g
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
ORDER BY g.id DESC
LIMIT $1;

-- name: ListGamesForOwner :many
--
-- **화면이 쓰는 쪽이다.** 위 ListGames 는 주인을 안 보므로 측정 전용이다.
--
-- 주인이 NULL(로그인 안 함)이면 익명 판만 보인다. 익명 판은 서로 구별할 수단이
-- 애초에 없으므로 지금까지와 같고, 갈리는 것은 **로그인한 판이 그 사람에게만
-- 보인다**는 쪽이다 (docs/02-architecture.md §7 위협 2).
--
-- **결과가 나온 판만 준다** (journal §51). 두는 중(result NULL)도, 중단된
-- 판(abandoned·declined)도 안 나간다 — 되짚을 것이 없는 줄이고, 중단된 판은 이어하기가
-- 가져갈 몫이다. 아래 GetGameForOwner 와 **같은 조건이어야 한다**: 목록에서만 빼면
-- `/reviews/<id>` 주소로 그냥 열린다(§46).
SELECT
    g.id,
    g.my_color,
    g.started_at,
    g.finished_at,
    g.result,
    (SELECT count(*) FROM game_moves m WHERE m.game_id = g.id) AS move_count,
    (SELECT count(*) FROM interventions i WHERE i.game_id = g.id) AS intervention_count,
    -- **대인전 판은 여기 그대로 뜬다.** 마이페이지의 집계에서만 빠진다(journal §83) —
    -- 그쪽은 개입 비율이 뜻을 갖는 자리이고, 목록은 「무엇을 뒀나」라 뜻이 다르다.
    g.match_id
FROM games g
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
  AND g.result IN ('win', 'loss', 'draw')
  AND g.user_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::bigint
ORDER BY g.id DESC
LIMIT $1;

-- name: GetGame :one
--
-- **여기서는 개입을 세지 않는다.** 어차피 아래에서 전부 읽어 오므로, 따로 센 숫자와
-- 실제로 온 줄 수가 두는 중인 판에서 어긋날 수 있다 — 목록(ListGames)은 줄을 안 읽으니
-- 거기서만 센다.
SELECT id, my_color, started_at, finished_at, result, start_sfen, opening_tag, match_id
FROM games
WHERE id = $1;

-- name: GetGameForOwner :one
--
-- 주인이 아니면 **0행**이다. 부르는 쪽에서 그것이 404가 된다 — 403이면 「그 번호의
-- 판이 있다」를 알려주는 셈이라, 남의 판 개수를 세어 볼 수 있다.
--
-- **끝나지 않은 판도 0행이다** — ListGamesForOwner 와 같은 조건이고, 같은 이유로 404다.
-- 「있지만 못 본다」를 알려주는 순간 중단된 판의 존재가 새어 나간다.
SELECT id, my_color, started_at, finished_at, result, start_sfen, opening_tag, match_id
FROM games
WHERE id = $1
  AND result IN ('win', 'loss', 'draw')
  AND user_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::bigint;

-- ─── 이어하기 ───────────────────────────────────────────────
-- 근거와 정한 것 셋은 journal §46 · §51.
--
-- **로그인한 사람만이다.** 셋 다 주인을 `=` 로 받아 익명 판(user_id NULL)이 애초에
-- 안 걸린다 — 익명끼리는 구별할 수단이 없어서(002_anonymous_games.sql) 「누구의 중단된
-- 판인가」에 답할 수가 없다.

-- name: ResumableGameForOwner :one
--
-- 이어할 수 있는 판 하나. **가장 최근 것 하나만 준다** — 목록을 주면 사람이 「어느 판을
-- 이어할까」를 고르는 화면이 되는데, 물음은 「두던 판을 이어할까」 하나다.
--
-- 한 수도 안 둔 판은 뺀다(ListGames 와 같은 EXISTS). 연결만 열렸다 끊긴 판이 그렇게
-- 남는데, 그것을 이어하는 것은 새 판을 여는 것과 같다.
SELECT
    g.id,
    g.my_color,
    g.started_at,
    g.opening_tag,
    (SELECT count(*) FROM game_moves m WHERE m.game_id = g.id) AS move_count
FROM games g
WHERE g.user_id = $1
  AND g.result = 'abandoned'
  AND EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
  -- **대인전 판은 이어할 수 없다**(journal §83). 이어하기는 엔진 대국의 장치라, 여기에
  -- 대인전 판이 걸리면 사람과 두던 수순이 **엔진 세션으로 이어진다** — 그 판의 두 행이
  -- 그 자리에서 갈라져(한쪽만 자라고 다른 쪽은 abandoned 로 남는다) 같은 대국이 아니게 된다.
  --
  -- 배포가 대국 중에 끼면 실제로 그 상태가 만들어진다: 테이블이 접히면서(abort) 수가
  -- 있는 행이 abandoned 로 닫히고, 그것이 정확히 이 질의의 조건이다.
  AND g.match_id IS NULL
ORDER BY g.id DESC
LIMIT 1;

-- name: ClaimGameForResume :one
--
-- **점유와 되열기가 한 문장이다.** `result = 'abandoned'` 를 조건에 두었으므로 두 번째
-- 요청은 0행을 받는다 — 탭 두 개가 같은 판을 동시에 이어하려 할 때 뒤엣것이 여기서
-- 걸리고, 그래야 세션 goroutine 둘이 한 대국 행에 기록을 겹쳐 쓰지 않는다.
--
-- finished_at 도 같이 지운다. 안 지우면 「끝난 시각이 있는데 두는 중인 판」이 남는다.
UPDATE games
SET result = NULL, finished_at = NULL
WHERE id = $1
  AND user_id = $2
  AND result = 'abandoned'
  -- **위 질의와 같은 조건이 여기에도 있어야 한다.** 목록에서만 빼면 `/ws/game?resume=<id>`
  -- 로 번호를 직접 넣어 그대로 열린다 — 되짚기의 목록과 상세가 같은 조건을 갖는 것과
  -- 같은 자리다(GetGameForOwner).
  AND match_id IS NULL
RETURNING id, my_color, start_sfen, opening_tag;

-- name: DeclineResume :execrows
--
-- 「いいえ」다. **행을 지우지 않는다** — 그 판의 개입과 기보가 실력 추정의 원본이고
-- (docs/01-core.md §5), 사람이 안 이어하겠다고 한 것과 기록을 버리는 것은 다른 일이다.
--
-- `declined` 는 abandoned 의 하위 상태다: 중단된 채로 끝났고 **사람이 그러기로 정했다**.
-- 갈라 두는 이유는 하나뿐이다 — 이걸 다시 물어보지 않기 위해서다.
UPDATE games
SET result = 'declined'
WHERE id = $1
  AND user_id = $2
  AND result = 'abandoned';

-- name: ListGameMoves :many
--
-- eval_cp 는 **先手 관점**이고 NULL일 수 있다(store.RecordedMove).
SELECT ply, usi, eval_cp FROM game_moves WHERE game_id = $1 ORDER BY ply;

-- name: ListGameInterventions :many
--
-- 같은 ply에 여러 행이 온다(InsertIntervention). id 로 이어 정렬해 **물러진 순서**를
-- 지킨다 — 한 국면에서 두 번 걸렸을 때 어느 쪽이 먼저였는지가 곧 이야기다.
SELECT ply, kind, category, delta_win, level_bucket, retracted_usi, best_cp, after_cp
FROM interventions
WHERE game_id = $1
ORDER BY ply, id;

-- name: CountGameResultsForOwner :many
--
-- 마이페이지의 전적. **결과가 나온 판만** 세는 것은 ListGamesForOwner 와 같은 규칙이다 —
-- 목록에 안 보이는 판이 전적에는 들어가면 두 화면이 같은 사람에 대해 다른 수를 말한다.
--
-- **한 수도 안 둔 판을 빼는 것도 같은 이유다**(그쪽의 EXISTS).
SELECT g.result, count(*) AS games
FROM games g
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
  AND g.result IN ('win', 'loss', 'draw')
  AND g.user_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::bigint
  -- **대인전 판은 안 센다**(match_id IS NULL). 개입이 없는 판이라 분모에만 들어가고,
  -- 그러면 「崩れやすいところ」의 비율이 그만큼 희석된다(journal §83).
  AND g.match_id IS NULL
GROUP BY g.result;

-- name: CountInterventionCategoriesForOwner :many
--
-- 마이페이지의 약점. **판을 가로질러 센다** — 총평은 한 판 안에서 세지만(server/summary.go)
-- 「무엇이 약한가」는 한 판으로 답할 것이 아니다.
--
-- 거르는 조건이 위와 같아야 한다: 전적에 안 들어간 판의 개입이 약점에는 들어가면
-- 같은 화면의 두 숫자가 다른 모집단을 센다.
SELECT i.category, count(*) AS hits
FROM interventions i
JOIN games g ON g.id = i.game_id
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
  AND g.result IN ('win', 'loss', 'draw')
  AND g.user_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::bigint
  -- **대인전 판은 안 센다**(match_id IS NULL). 개입이 없는 판이라 분모에만 들어가고,
  -- 그러면 「崩れやすいところ」의 비율이 그만큼 희석된다(journal §83).
  AND g.match_id IS NULL
GROUP BY i.category;

-- ─── 무르기 ─────────────────────────────────────────────────
-- 사람이 스스로 무른 수. 개입과 갈라 두는 이유는 008_game_undos.sql.

-- name: InsertUndo :exec
--
-- **평가치는 인자로 안 받는다.** 그 값은 판정이 `game_moves` 에 이미 채워 뒀거나 아직
-- 안 채웠거나 둘 중 하나이고, 세션이 그것을 다시 들고 다니면 같은 숫자가 두 벌이 된다.
-- 여기서 옮겨 담고 아래 `DeleteMovesFrom` 이 원본을 지운다 — 순서가 뒤집히면 NULL이 남는다.
INSERT INTO game_undos (game_id, ply, usi, eval_cp)
VALUES ($1, $2, $3, (SELECT eval_cp FROM game_moves WHERE game_id = $1 AND ply = $2));

-- name: DeleteMovesFrom :exec
--
-- 무르기가 지우는 것은 **사람의 수와 그 뒤 상대의 응수까지**다. 手数로 자르는 것이
-- 요점이다 — 개수로 세면 기보에 구멍이 있을 때 엉뚱한 수가 남는다(review.detailOf 의
-- 같은 판단).
DELETE FROM game_moves WHERE game_id = $1 AND ply >= $2;

-- name: ListGameUndos :many
--
-- 같은 ply에 여러 행이 온다(무르고 다시 두고 또 무른 경우). id 로 이어 정렬해 **무른
-- 순서**를 지킨다 — ListGameInterventions 와 같은 규약이다.
SELECT ply, usi, eval_cp FROM game_undos WHERE game_id = $1 ORDER BY ply, id;

-- name: CountGameUndos :one
--
-- **이어하는 판이 3회 제한을 리셋하지 않게** 한다(game.Config.UndoUsed). 세션은 연결에
-- 매여 있어 이어할 때마다 새로 서는데, 카운터도 같이 0이 되면 제한이 제한이 아니다.
SELECT count(*) FROM game_undos WHERE game_id = $1;

-- ─── 전법·囲い ───────────────────────────────────────────────
-- 사람이 그 판에서 실제로 짠 이름. 근거는 009_game_style_tags.sql.

-- name: AddGameStyleTag :exec
--
-- **같은 이름을 두 번 안 담는다.** 囲い는 판에서 매번 다시 세어지므로(game.styleTags) 한 판에
-- 같은 코드가 수십 번 온다 — 세션이 본 것을 기억해 거르지만, 이어하는 판은 그 기억을 잃는다
-- (세션이 연결에 매여 있다, §51). 그래서 거르는 자리를 **여기에도** 둔다.
UPDATE games
SET style_tags = array_append(style_tags, @code::text)
WHERE id = @game_id AND NOT (style_tags @> ARRAY[@code::text]);

-- name: CountGameStyleTagsForOwner :many
--
-- 마이페이지의 「짠 진형」. **판 수를 센다** — 한 판에서 같은 이름이 여러 번 나오는 일은
-- 위 질의가 이미 막았으므로, 이 숫자는 언제나 「그 이름으로 둔 판이 몇 판인가」다.
--
-- 거르는 조건이 전적·약점과 **같아야 한다**: 셋이 한 화면에 서는데 모집단이 갈리면
-- 「12판 뒀는데 진형은 30판에서 나온 것」이 된다.
-- **`::text` 를 적어야 한다.** `unnest` 의 결과 타입을 sqlc 가 못 읽어 `interface{}` 로
-- 만들고, 그러면 코드가 문자열인지 아닌지를 부르는 쪽이 매번 확인해야 한다.
SELECT t.code::text AS code, count(*) AS games
FROM games g
CROSS JOIN LATERAL unnest(g.style_tags) AS t(code)
WHERE EXISTS (SELECT 1 FROM game_moves m WHERE m.game_id = g.id)
  AND g.result IN ('win', 'loss', 'draw')
  AND g.user_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::bigint
  -- **대인전 판은 안 센다**(match_id IS NULL). 개입이 없는 판이라 분모에만 들어가고,
  -- 그러면 「崩れやすいところ」의 비율이 그만큼 희석된다(journal §83).
  AND g.match_id IS NULL
GROUP BY t.code;

-- ─── 부른 힌트 ───────────────────────────────────────────────
-- 사람이 불러서 받은 최선수 힌트. 개입과 갈라 두는 이유는 010_game_hints.sql.

-- name: InsertHint :exec
INSERT INTO game_hints (game_id, ply, sfen_key, stage, best_usi)
VALUES ($1, $2, $3, $4, $5);

-- name: CountGameHints :one
--
-- **이어하는 판이 6회 제한을 리셋하지 않게** 한다(game.Config.HintsUsed) — `CountGameUndos`
-- 와 같은 자리, 같은 이유다.
SELECT count(*) FROM game_hints WHERE game_id = $1;

-- name: CountGameHintStages :many
--
-- 국면마다 **몇 단계까지 열렸나**. 이어하는 판이 이것으로 「이 국면은 이미 답을 봤다」를
-- 되찾는다 — 세션의 기억은 연결과 함께 사라진다(§51).
SELECT sfen_key, max(stage)::int AS stage
FROM game_hints
WHERE game_id = $1
GROUP BY sfen_key;

-- name: MarkHintTaken :exec
--
-- 알려준 수를 실제로 뒀는가. **그 국면의 줄 전부에 적는다** — 단계가 둘이면 행이 둘인데,
-- 사람이 답을 본 것은 한 번이라 둘이 같은 답을 가져야 한다.
UPDATE game_hints
SET taken = @taken
WHERE game_id = @game_id AND sfen_key = @sfen_key;
