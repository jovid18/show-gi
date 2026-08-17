-- 되짚기 퀴즈. 설계 근거는 journal §53.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).

-- 한 판에 한 행이다. **문항 전체를 jsonb 하나로 둔다.**
--
-- 읽는 쪽이 늘 통째로 읽는 것도 이유지만, 결정적인 것은 詰み 문항이 **트리**라는 것이다 —
-- 행으로 쪼개면 채점 질의가 그 트리 모양을 SQL에서 다시 만들어야 하고, 그 모양은
-- internal/quiz 가 이미 알고 있다.
CREATE TABLE game_quizzes (
    game_id bigint PRIMARY KEY REFERENCES games ON DELETE CASCADE,
    -- 만든 생성기의 판. 올리면 옛 행이 **무시된다**(다시 만들지 않는다 — 생성은 판이
    -- 끝나는 자리에만 있다). 문항 기준이 바뀌면 옛 문항은 그 기준으로 만든 것이 아니라서
    -- 채점 규약이 어긋난다. 총평의 `summaryPromptVersion` 과 같은 규약이다.
    version      int         NOT NULL,
    payload      jsonb       NOT NULL,
    generated_at timestamptz NOT NULL DEFAULT now()
);
