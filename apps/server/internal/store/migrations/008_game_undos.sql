-- 사람이 스스로 무른 수. 설계 근거는 docs/06-status.md §72.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).

-- `interventions` 에 kind 를 하나 더 붙이지 않고 표를 새로 판다.
--
-- 저쪽의 `retracted_usi` 는 「**개입에 오염되지 않은** 유일한 실력 신호」로 정의돼 있고
-- (docs/01-core.md §5), 사람이 스스로 무른 수를 그 칸에 섞으면 그 문장이 그 자리에서
-- 거짓이 된다 — 상수 재채점(§39)의 모집단도 같이 오염된다. 목록의 「개입 N회」가
-- `COUNT(interventions)` 인 것도 같은 이유로 갈라야 한다: 무른 것은 개입이 아니다.
CREATE TABLE game_undos (
    id      bigserial PRIMARY KEY,
    game_id bigint    NOT NULL REFERENCES games ON DELETE CASCADE,
    -- 무른 **사람 수의 手数**다. 그 수는 기보에서 지워지므로 여기서만 남는다.
    -- 같은 手数가 여러 번 올 수 있다 — 무르고 다시 두고 또 무르면 그렇게 된다.
    ply int  NOT NULL,
    usi text NOT NULL,
    -- 그 수 뒤의 **先手 관점** cp. `game_moves.eval_cp` 에서 옮겨 온다(같은 규약).
    -- 판정이 아직 그 手数를 안 채웠으면 NULL 이다 — 0(호각)과 다르다.
    eval_cp    int,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_undos_game_idx ON game_undos (game_id, ply);
