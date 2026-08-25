-- 미리 재는 手의 줄(018). 메모리 채널이던 것을 표로 내렸고, 근거는 journal §115.
--
-- 모든 잠금이 SKIP LOCKED 다 — 워커가 서로를 기다리는 자리를 만들지 않는다
-- (LockQueueCandidates 와 같은 계열).

-- name: EnqueueAnalysisPly :exec
--
-- 방금 둔 手를 줄에 세운다. 착수 경로가 이 문장을 기다리지 않는다 — 부르는 쪽이
-- 배수구 goroutine 이다(matchAnalyzer.drain).
--
-- 두 번 세워도 한 행이다(ON CONFLICT). 착수 경로가 같은 手를 두 번 부를 수 있고
-- (matchRecords.counting 의 뒤따라온 자리) 같은 국면을 두 번 재는 것은 그대로 낭비다.
--
-- 그만둔 판에는 안 쌓는다(NOT EXISTS). 안 걸면 그 판의 남은 手가 전부 행이 되어
-- 밀린 양으로 세어지는데, 아무도 그것을 재지 않으므로 백로그가 안 내려온다.
INSERT INTO analysis_plies (match_id, ply, start_sfen, moves)
SELECT sqlc.arg(match_id)::text,
       sqlc.arg(ply)::int,
       sqlc.arg(start_sfen)::text,
       sqlc.arg(moves)::text[]
WHERE NOT EXISTS (
    SELECT 1 FROM analysis_plies d
    WHERE d.match_id = sqlc.arg(match_id)::text AND d.dead
)
ON CONFLICT (match_id, ply) DO NOTHING;

-- name: ClaimAnalysisPly :one
--
-- 안 잰 手 하나를 집는다. 없으면 0행이다.
--
-- 리스를 시각으로 받는다($1 = 지금 - 리스). 상수를 SQL 에 안 적는 것은 큐가 밴드를
-- 안 적은 것과 같은 이유다 — 같은 값이 Go 와 SQL 에 두 벌 있으면 한쪽이 조용히 낡는다.
--
-- 낡은 리스를 도로 집는 것이 이 문장의 두 번째 일이다. 워커가 배포나 스팟 회수로
-- 사라지면 그 手는 claimed_at 만 찍힌 채 남고, 그 시각이 낡으면 여기가 되찾는다.
--
-- 고르는 쪽을 MATERIALIZED CTE 로 못 박는다. `IN (SELECT ... LIMIT 1 FOR UPDATE)` 로 쓰면
-- 계획에 따라 그 서브쿼리가 바깥 행마다 다시 돌아 여러 행을 잠그는데, 돌려받는 것은
-- 한 행이라 나머지는 아무도 재지 않은 채 「집힌」 상태로 남는다. 워커 하나로 재 봤더니
-- 그 값이 여덟까지 갔다(journal §115).
WITH next AS MATERIALIZED (
    SELECT p.match_id, p.ply FROM analysis_plies p
    WHERE p.done_at IS NULL AND NOT p.dead
      AND (p.claimed_at IS NULL OR p.claimed_at < sqlc.arg(lease_before)::timestamptz)
    ORDER BY p.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE analysis_plies t SET claimed_at = now()
FROM next n
WHERE t.match_id = n.match_id AND t.ply = n.ply
RETURNING t.match_id, t.ply, t.start_sfen, t.moves;

-- name: FinishAnalysisPly :exec
--
-- 잰 값을 그 행에 적는다.
--
-- 행이 없으면 아무 일도 안 일어난다. 그것이 규약이다 — 판이 끝나 자리가 걷힌 뒤에
-- 도착한 늦은 측정이 판을 되살리면 그 항목을 아무도 안 지운다(journal §106).
UPDATE analysis_plies
SET done_at   = now(),
    before_cp = $2,
    after_cp  = $3,
    blunder   = $4,
    delta_win = $5,
    threshold = $6,
    decided   = $7
WHERE match_id = $1 AND ply = $8 AND done_at IS NULL;

-- name: StopAnalysisAhead :exec
--
-- 그 판을 미리 재는 것을 그만둔다. 부르는 자리가 둘이고 이유가 다르다 — 한 手가 실패했거나
-- (뒤도 전부 같은 자리에서 실패한다), 판이 끝나 남은 手를 analyze 가 맡거나다.
--
-- 아직 안 잰 행만 표시한다. 이미 잰 값은 판이 끝날 때 그대로 쓰이고, 여기서 같이 덮으면
-- 그만큼을 다시 재게 된다.
--
-- 이 표시가 밀린 양의 정본을 하나로 만든다. 안 하면 판이 끝난 뒤 남은 手가 표에도 남고
-- queuedPlies 에도 더해져 **같은 手가 두 번 세어진다**(journal §116).
UPDATE analysis_plies SET dead = true
WHERE match_id = $1 AND done_at IS NULL;

-- name: MeasuredAnalysisPlies :many
--
-- 그 판에서 미리 재 둔 것을 한 번에 읽는다.
--
-- 手마다 묻지 않는다. 판이 끝나는 자리에서 手数만큼 왕복하면 그 자체가 밀리는 값이고,
-- 이 표는 판 하나가 곧 한 묶음이라 한 번에 읽는 것이 자연스럽다.
SELECT ply, before_cp, after_cp, blunder, delta_win, threshold, decided
FROM analysis_plies
WHERE match_id = $1 AND done_at IS NOT NULL
ORDER BY ply;

-- name: CountMeasuredAnalysisPlies :one
--
-- 그 판에서 미리 재 둔 手数다. 판이 끝날 때 남은 일의 크기를 이것으로 센다.
SELECT count(*) FROM analysis_plies
WHERE match_id = $1 AND done_at IS NOT NULL;

-- name: CountAnalysisBacklog :one
--
-- 아직 안 잰 手数다. AnalysisBacklogPlies 가 이 값이고, 오토스케일의 신호가 된다.
--
-- 그만둔 판은 안 센다. 아무도 재지 않을 것을 세면 백로그가 안 내려오고, 그 위에서
-- 스케일 판단이 돈다.
SELECT count(*) FROM analysis_plies
WHERE done_at IS NULL AND NOT dead;

-- name: DiscardAnalysisMatch :exec
--
-- 그 판의 행을 걷는다. 판이 끝난 뒤와, 반쪽이라 분석하지 않는 자리에서 부른다.
DELETE FROM analysis_plies WHERE match_id = $1;

-- name: SweepAnalysisPlies :exec
--
-- 낡은 행을 걷는다. 판이 비정상으로 끝나면 DiscardAnalysisMatch 가 안 돌고, 그때
-- 남는 행이 이 표의 유일한 누수다.
DELETE FROM analysis_plies WHERE created_at < $1;

-- 끝난 판의 줄(019). 자리는 games 가 들고 여기는 「무엇이 남았나」만 든다.

-- name: HoldAnalysisJob :exec
--
-- 판의 자리를 미리 세운다. 手数는 아직 비어 있어 집히지 않는다.
--
-- 번호가 나가기 전에 서야 한다 — 화면이 되짚기를 여는 순간 이미 「분석 중」이라야 하고,
-- 그 시점에는 자리가 하나뿐일 수 있다(matchRecords.collect).
INSERT INTO analysis_jobs (match_id) VALUES ($1)
ON CONFLICT (match_id) DO NOTHING;

-- name: ReadyAnalysisJob :exec
--
-- 자리가 다 찼다. 手数를 적으면 그때부터 집힌다.
--
-- HoldAnalysisJob 이 세운 행을 채우는 것이 보통인데, 없으면 여기서 만든다. UPDATE 로만
-- 두면 그 앞이 한 번 실패했을 때 이 문장이 **조용히 아무 일도 안 하고** 그 판이 줄에
-- 안 선다 — 되짚기는 그것을 「남지 않았다」로만 보여 주므로 아무도 못 알아챈다.
INSERT INTO analysis_jobs (match_id, plies) VALUES ($1, $2)
ON CONFLICT (match_id) DO UPDATE SET plies = EXCLUDED.plies;

-- name: ClaimAnalysisJob :one
--
-- 자리가 찬 판 하나를 집는다. 없으면 0행이다. ClaimAnalysisPly 와 같은 모양이다.
WITH next AS MATERIALIZED (
    SELECT j.match_id FROM analysis_jobs j
    WHERE j.plies IS NOT NULL
      AND (j.claimed_at IS NULL OR j.claimed_at < sqlc.arg(lease_before)::timestamptz)
    ORDER BY j.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE analysis_jobs t SET claimed_at = now()
FROM next n WHERE t.match_id = n.match_id
RETURNING t.match_id, t.plies;

-- name: DropAnalysisJob :exec
--
-- 그 판을 줄에서 걷는다. 다 재고 나서와, 반쪽이라 분석하지 않는 자리에서 부른다.
DELETE FROM analysis_jobs WHERE match_id = $1;

-- name: AnalysisJobBacklog :one
--
-- 아직 안 집힌 판의 수와 그 판들이 안 잰 手数다. 밀린 양의 판 몫과 手 몫이 이 한 행이다.
--
-- 집힌 판은 안 센다. 그것은 지금 도는 일이지 밀린 일이 아니다 — 리스가 낡으면 다시 센다.
SELECT count(*) AS games, coalesce(sum(plies), 0)::bigint AS plies
FROM analysis_jobs
WHERE plies IS NOT NULL
  AND (claimed_at IS NULL OR claimed_at < sqlc.arg(lease_before)::timestamptz);

-- name: IsGameAnalyzing :one
--
-- 그 판이 아직 줄에 있거나 도는 중인가. 되짚기가 이 값으로 「분석 중」과 「남지 않았다」를
-- 가른다(server/review.go).
--
-- games 를 지나 찾는다. 자리를 표에 옮겨 적지 않기 때문이고, 그 조인은 games_match_idx 가 받는다.
SELECT EXISTS (
    SELECT 1 FROM analysis_jobs j
    JOIN games g ON g.match_id = j.match_id
    WHERE g.id = $1
);

-- name: MatchSeats :many
--
-- 그 판의 자리들이다. 대인전 한 판이 games 행 둘이고 그 행이 곧 자리다(012_match_games.sql).
--
-- 색으로 정렬한다. 기보를 첫 자리의 행 하나에서만 읽으므로(analyze) 순서가 흔들리면
-- 「이 판을 잴 수 있나」의 답이 실행마다 달라진다.
SELECT id, user_id, my_color FROM games
WHERE match_id = $1
ORDER BY my_color;

-- name: SweepAnalysisJobs :exec
--
-- 낡은 행을 걷는다. 자리가 영영 안 차는 반쪽 판이 이 표의 누수다.
DELETE FROM analysis_jobs WHERE created_at < $1;
