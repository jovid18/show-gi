-- 검토 화면에서 저장한 국면. 정한 것과 그 근거는 journal §96.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 있다.** 표 하나와 인덱스 하나를 더할 뿐이고, 이 파일 앞의 서버 이미지는
-- 이 표를 안 읽는다 — 순서를 어느 쪽으로 잡아도 안 깨진다.

BEGIN;

-- 국면이 아니라 수순을 든다. 手合割 id 하나와 0手目부터의 USI 한 줄이고, SFEN 칸이 없다.
--
-- 불러오기가 그 두 칸을 주소에 실어 `/api/explore` 로 다시 묻는다(server/explore.go). SFEN 을
-- 들면 저장한 값이 그대로 요청 본문이 되어 「아무 국면이나 깊이 12로 재 주는 공개 엔진」의
-- 문이 저장·불러오기 쪽으로 다시 열린다(journal §37).
CREATE TABLE explore_snapshots (
    id      bigserial PRIMARY KEY,
    -- 익명이 없다. 검토 자체가 로그인 뒤에 있고(server/explore.go 의 첫 번째 벽), 익명끼리는
    -- 구별할 수단이 없어서(002_anonymous_games.sql) 「내가 저장한 국면」이 성립하지 않는다.
    user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    name    text   NOT NULL,
    -- 빈 값이 平手다. internal/handicap 의 규약을 그대로 쓴다.
    handicap text  NOT NULL DEFAULT '',
    -- 양쪽 수가 전부 들어 있는 한 줄. 빈 배열이 0手目(시작 국면)이다.
    moves      text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 이름에 UNIQUE 를 안 건다. 같은 이름 둘이 서는 것보다 저장이 거절되는 것이 나쁘고,
-- 목록 줄이 手合割·手数·저장 시각을 같이 들어서 두 줄을 가를 수 있다.
--
-- 정렬 칸이 id 다. created_at 은 now() 라 같은 초에 여러 행이 들어가면 순서가 흔들리는데
-- (games 목록과 같은 판단) id 는 시퀀스라 그 자리에서 갈린다.
CREATE INDEX explore_snapshots_user_idx ON explore_snapshots (user_id, id DESC);

COMMIT;
