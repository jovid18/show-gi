-- 탐색 하나가 얼마나 걸렸는지를 남긴다. depth·k 별 비용을 나중에 질의로 보기 위한 자리다.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 더하는 것이 새 테이블 하나뿐이라
-- 이 파일 앞의 이미지는 그 테이블을 보지 않는다. 뒤의 이미지는 테이블이 없으면 시간 기록만
-- 건너뛴다 — 기록 실패가 탐색을 막지 않는다(archive.Searcher.recordTiming).

BEGIN;

-- 한 행이 탐색 한 번이다. 국면당 한 행이 아니라 부른 횟수만큼 쌓인다 — 같은 국면을
-- 다른 k로 다시 묻는 것이 이 표가 재려는 바로 그것이다.
--
-- positions 를 참조하지 않는다. 국면 행은 이 기록과 같은 비동기 경로에서 따로 쓰이고
-- 순서가 정해져 있지 않아, FK 를 걸면 먼저 도착한 쪽이 실패한다.
CREATE TABLE IF NOT EXISTS search_timings (
    id       bigserial PRIMARY KEY,
    sfen_key text NOT NULL,
    depth    int  NOT NULL,
    -- 부른 쪽이 요구한 MultiPV 다. 돌아온 후보 수가 아니다 — 합법수가 그보다 적으면
    -- 적게 온다(archive.wanted).
    k        int  NOT NULL,
    ms       int  NOT NULL,
    -- 캐시 히트면 엔진을 부르지 않은 것이다. 탐색 비용을 볼 때는 빼고 본다.
    cached     boolean     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 보는 질의가 depth·k 로 묶는 것 하나다.
CREATE INDEX IF NOT EXISTS search_timings_depth_k_idx ON search_timings (depth, k);

-- 추가만 되는 표다. 오래된 행을 잘라내는 것은 사람이 하고, 그때 이 인덱스를 쓴다.
CREATE INDEX IF NOT EXISTS search_timings_created_at_idx ON search_timings (created_at);

COMMIT;
