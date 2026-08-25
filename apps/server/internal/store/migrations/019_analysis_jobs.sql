-- 끝난 판의 줄. 프로세스 메모리에 있던 채널(analysisQueue)과 「분석 중」 표시(pending)를
-- 표로 내린 것이다. 근거는 journal §118.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 이 파일 앞의 이미지는 이 표를
-- 안 읽고, 뒤의 이미지는 표가 없으면 판을 줄에 못 세워 평가치가 안 채워진다 — 대국 자체는
-- 그대로 돈다.

BEGIN;

-- 판 하나가 한 행이다.
--
-- 자리(seats)를 안 싣는다. `games` 가 이미 든다 — 대인전 한 판이 games 행 둘이고
-- (012_match_games.sql) 그 행의 id·user_id·my_color 가 곧 자리다. 판이 시작될 때 만들어져
-- 안 바뀌므로 여기 옮겨 적으면 같은 사실이 두 벌이 된다.
--
-- 手数도 안 싣는다 — 아래 plies 는 「아직 안 잰 手」이지 판의 길이가 아니다.
CREATE TABLE analysis_jobs (
    -- 방 id 다. analysis_plies 와 같은 값이고 FK 는 양쪽 다 안 건다.
    match_id   text PRIMARY KEY,

    -- 아직 안 잰 手数. NULL 이 「자리가 아직 안 찼다」이고, 그 행은 집히지 않는다.
    --
    -- 두 단계인 이유는 화면이다. 번호가 나가는 순간 되짚기를 열 수 있고 그때 이미
    -- 「분석 중」이라야 하는데(matchRecords.collect), 그 시점에는 자리가 하나뿐일 수 있다.
    -- 그래서 행은 먼저 서고 手数는 두 자리가 다 온 뒤에 찬다.
    plies      int,

    -- 리스. analysis_plies 와 같은 규약이다 — 낡으면 다른 워커가 도로 집는다.
    -- 그것이 「배포 중에 죽은 워커의 판」을 되찾는 장치이고, 메모리 채널에는 없던 것이다.
    claimed_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now()
);

-- 부분 인덱스다. 워커가 보는 것은 자리가 찬 행뿐이고, 안 찬 행은 match_id 로 직접 읽힌다.
CREATE INDEX analysis_jobs_ready_idx ON analysis_jobs (created_at)
    WHERE plies IS NOT NULL;

COMMIT;
