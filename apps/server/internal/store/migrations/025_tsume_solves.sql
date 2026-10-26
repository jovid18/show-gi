-- 詰め将棋 풀이의 공유 캐시와 사람마다의 풀이 기록. 정한 것과 그 근거는 journal §155.
--
-- 실행은 사람이 DB 클라이언트로 직접 한다 (deploy/README.md §4).
--
-- 되돌릴 수 있고 순서를 어느 쪽으로 잡아도 깨지지 않는다. 표 둘을 더할 뿐이고, 이 파일 앞의
-- 서버 이미지는 두 표를 읽지 않는다. 뒤의 이미지는 표가 없으면 쓰기·읽기 실패를 로그로 남기고
-- 매번 다시 푼다(server/tsume.go).

BEGIN;

-- 국면 하나의 답. mate_positions 와 같은 이유로 사람에게 매이지 않는다. 풀이가 마디 수로
-- 끊기는 결정적 탐색이라 같은 국면·같은 풀이 판이면 누가 풀어도 답이 같다.
--
-- solver 가 키에 들어간다(tsume.Version). 풀이를 고쳐 답이 바뀌면 옛 판의 행은 읽히지 않고
-- 새 판의 행이 곁에 쌓인다. 지우지 않는 것은 positions 의 computed_depth 와 같은 판단이다.
--
-- 증명된 답만 든다. 「調べきれず」는 상한을 올리거나 풀이를 고치면 바뀌는 답이라 남기지
-- 않는다(mate_positions 가 timeout 을 남기지 않는 것과 같다).
CREATE TABLE tsume_results (
    -- 手数를 뺀 SFEN(shogi.PositionKey).
    sfen_key   text        NOT NULL,
    solver     int         NOT NULL,
    status     text        NOT NULL CHECK (status IN ('mate', 'nomate')),
    -- mate 일 때만 값이 있다.
    plies      int,
    shortest   boolean     NOT NULL DEFAULT false,
    -- 화면이 받는 트리 그대로다(server/tsume.go 의 tsumeMove). mate 일 때만 값이 있다.
    tree       jsonb,
    nodes      bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sfen_key, solver)
);

-- 사람이 푼 국면. 풀 때마다 서버가 남긴다. 같은 국면을 다시 풀면 새 행을 만들지 않고
-- solved_at 만 고친다.
--
-- 트리를 들지 않는다. 다시 열면 화면이 같은 국면으로 풀기를 다시 부르고, 그 요청이 위 캐시에서
-- 답한다. 여기에 두 벌을 두면 풀이를 고친 뒤 옛 트리가 기록 쪽에만 남는다.
CREATE TABLE tsume_solves (
    id        bigserial   PRIMARY KEY,
    user_id   bigint      NOT NULL REFERENCES users ON DELETE CASCADE,
    sfen_key  text        NOT NULL,
    -- 빈 값이면 화면이 결과(「7手詰め」)를 이름 자리에 쓴다.
    name      text        NOT NULL DEFAULT '',
    -- 마지막으로 풀었을 때의 답. 목록 한 줄에 쓴다. unknown 도 든다.
    status    text        NOT NULL CHECK (status IN ('mate', 'nomate', 'unknown')),
    plies     int,
    solved_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, sfen_key)
);

-- 목록 순서. 같은 시각이면 id 가 가른다(explore_snapshots 와 같은 판단).
CREATE INDEX tsume_solves_user_idx ON tsume_solves (user_id, solved_at DESC, id DESC);

COMMIT;
