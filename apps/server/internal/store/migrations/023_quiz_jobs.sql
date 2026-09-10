-- 되짚기 문항을 만드는 줄. 판을 재는 줄(019)에 얹혀 있던 것을 떼어낸 자리다.
-- 근거는 journal §138.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 이 파일 앞의 이미지는 이 표를
-- 읽지 않고 문항을 그 자리에서 만든다. 뒤의 이미지는 표가 없으면 문항을 세우지 못해
-- 되짚기가 「準備中」에 머무른다. 대국도 평가치도 그대로 돈다.

BEGIN;

-- 판 하나가 한 행이다. 문항이 판마다 하나이기 때문이고(game_quizzes 도 같은 키다),
-- 그래서 手 단위인 018 과 달리 병렬성을 여기서 더 쪼갤 자리가 없다.
--
-- 자리도 수순도 싣지 않는다. 019 가 자리를 games 에 맡긴 것과 같은 판단이고, 여기는
-- 그보다 더 적어도 된다 — 문항 생성기가 받는 것이 games 행 하나에서 읽은 기록뿐이다.
CREATE TABLE quiz_jobs (
    -- games 를 참조한다. 019 의 match_id 와 갈리는 자리다. 저쪽은 방 id 라 표가 없지만
    -- 이쪽은 판 번호라, 판이 지워지면 만들 문항도 없다.
    game_id bigint PRIMARY KEY REFERENCES games ON DELETE CASCADE,

    -- 리스. 018·019 와 같은 규약이다. 낡으면 다른 워커가 도로 집는다.
    --
    -- 값이 두 큐보다 짧을 수 없다. 문항 하나에 최대 5분을 주므로(server 의 quizTimeout)
    -- 그 안에 끝나거나 끊긴다.
    claimed_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now()
);

-- 집는 질의가 오래된 것부터 본다. 부분 인덱스가 아닌 것은 019 와 갈리는 자리다 —
-- 저쪽은 자리가 차기를 기다리는 행이 있지만, 여기 선 행은 전부 곧바로 집힌다.
CREATE INDEX quiz_jobs_ready_idx ON quiz_jobs (created_at);

COMMIT;
