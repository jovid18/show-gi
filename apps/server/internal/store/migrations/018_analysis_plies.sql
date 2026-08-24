-- 미리 재는 手의 줄. 프로세스 메모리에 있던 채널(prefetchQueue)을 표로 내린 것이다.
-- 근거는 journal §115.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 이 파일 앞의 이미지는 이 표를
-- 안 읽고, 뒤의 이미지는 표가 없으면 미리 재기를 통째로 건너뛴다 — 그 手는 판이 끝날 때
-- 그 자리에서 재어지므로(match_analysis.go 의 analyze) 잃는 사실이 없다.

BEGIN;

-- 手 하나가 한 행이다. 판 단위로 두지 않는 이유는 병렬성이다 — 手는 서로를 안 보므로
-- (game.engineAnalyst.Judge 가 startSFEN + moves[:ply] 만 받는다) 워커가 몇이든 같은
-- 판의 서로 다른 手를 동시에 잰다. 판 단위면 소비자를 늘려도 동시 진행 판 수에서 막힌다.
--
-- 결과 칸까지 같은 행에 든다. 메모리에서 aheadOfMatch.plies 가 하던 일이고, 갈라 두면
-- 「재는 것」과 「잰 것」이 두 표가 되는데 둘의 수명이 정확히 같다.
CREATE TABLE analysis_plies (
    -- 방 id 다(internal/match 의 영숫자 8자). games.match_id 와 같은 값이지만 FK를 안 건다 —
    -- 이 줄은 판이 끝나기 전부터 자라고, 그때 games 행이 이미 있는지를 이 표가 알 필요가 없다.
    match_id   text NOT NULL,
    ply        int  NOT NULL,

    -- 재는 데 필요한 입력 전부. game_moves 에서 읽지 않고 여기 싣는 이유는 그쪽에 구멍이
    -- 날 수 있기 때문이다 — 기록기는 큐가 차면 이벤트를 버리고 계속한다(dbRecorder.send).
    -- 세우는 쪽은 테이블 goroutine 이 들고 있는 수순이라 구멍이 없다.
    start_sfen text   NOT NULL,
    moves      text[] NOT NULL,

    -- dead 는 이 판을 미리 재는 것을 그만뒀다는 표시다. 한 手가 실패하면 뒤의 手도 전부
    -- 같은 자리에서 실패하므로(analyze 의 같은 판단) 안 그만두면 남은 手数만큼 탐색을 버린다.
    dead       boolean NOT NULL DEFAULT false,

    -- claimed_at 은 리스다. 워커가 집을 때 지금으로 놓고, 이 값이 낡으면 다른 워커가
    -- 도로 집는다 — 그것이 「배포 중에 죽은 워커의 手」를 되찾는 유일한 장치다.
    claimed_at timestamptz,
    -- done_at 이 NULL 인 것이 곧 밀린 手다. AnalysisBacklogPlies 가 이 조건을 센다.
    done_at    timestamptz,

    -- 아래는 다 재고 나서 찬다. done_at 이 NULL 이면 뜻이 없다.
    --
    -- 평가치 둘은 先手 관점이다(game.Judgement.SenteCpBefore·SenteCpAfter). 나머지 넷은
    -- skill.Move 가 먹는 값 그대로다 — blunder 를 낙폭에서 다시 구할 수 없어서 싣는다.
    -- 종반 판정으로 걸린 手는 승률이 포화한 구간에서 나와 낙폭이 0에 가깝다(skill.Move).
    before_cp  int,
    after_cp   int,
    blunder    boolean,
    delta_win  double precision,
    threshold  double precision,
    decided    boolean,

    -- 판이 비정상으로 끝나면 discard 가 안 돈다. 그때 남는 행을 걷는 데 쓴다.
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (match_id, ply)
);

-- 부분 인덱스다. 워커가 보는 것은 아직 안 잰 행뿐이고, 잰 행은 판이 끝날 때 match_id 로
-- 한 번 읽히고 지워진다.
--
-- 정렬 칸이 created_at 이다. 먼저 둔 手를 먼저 재야 판이 끝나는 시점에 남은 일이 가장 적다.
CREATE INDEX analysis_plies_todo_idx ON analysis_plies (created_at)
    WHERE done_at IS NULL AND NOT dead;

COMMIT;
