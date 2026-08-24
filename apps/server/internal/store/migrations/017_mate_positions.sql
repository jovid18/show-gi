-- 詰み 탐색 결과 캐시. 탐색부의 positions 와 같은 자리를 詰将棋 solver 에게 준다.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).

-- positions 에 컬럼을 더하지 않고 표를 따로 둔다. 「더 깊은 것이 이긴다」 규칙이 둘이고
-- 서로 독립이기 때문이다 — 탐색은 computed_depth 와 후보 수로, 詰み은 depth_limit 으로
-- 갈린다. ON CONFLICT 하나에 두 규칙을 넣으면 한쪽 사실이 다른 쪽 갱신에 지워진다.
--
-- positions 를 FK로 참조하지 않는다. 詰み 답은 그 국면을 탐색한 적이 있는지와 무관하게
-- 참이고, 참조를 걸면 퀴즈가 훑는 국면마다 후보 없는 positions 행을 먼저 만들어야 한다.
CREATE TABLE mate_positions (
    -- 手数를 뺀 SFEN. positions.sfen_key 와 같은 형태다(shogi.PositionKey).
    sfen_key    text PRIMARY KEY,
    -- 이 답을 낸 solver 의 手数 한계(ENGINE_MATE_PLIES). 읽는 쪽이 이 값을 견준다 —
    -- 한계 9의 「詰み이 없다」는 한계 11의 「없다」가 아니다.
    depth_limit int NOT NULL,
    -- 증명된 詰み 수순. 빈 배열이면 증명된 「詰み이 없다」다.
    --
    -- checkmate timeout 은 여기 안 들어온다. 「이 한계 안에서는 모른다」이지 「없다」가
    -- 아니라서, 없다고 저장하면 있는 詰み을 놓친 채 종반 판정이 돈다(01-core.md §2).
    -- 그래서 행이 없는 것이 곧 「모른다」다 — 규칙을 주석이 아니라 스키마가 든다.
    moves       text[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
