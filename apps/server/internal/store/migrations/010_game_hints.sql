-- 사람이 **불러서** 받은 최선수 힌트. 설계 근거는 docs/06-status.md §78.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).

-- `game_undos` 와 같은 모양이다. 예산이 **판에 붙고 세션에 안 붙기** 때문이다 — 세션은
-- 연결에 매여 있어 이어할 때마다 새로 서는데, 카운터가 그때 0으로 돌아가면 판당 6회가
-- 「연결당 6회」가 되고 그건 제한이 아니다(008_game_undos.sql 이 같은 자리에서 같은 말을 한다).
--
-- **`interventions` 에 안 넣는다.** 저쪽은 「앱이 먼저 말을 건 자리」이고 이쪽은 사람이
-- 부른 자리라, 섞으면 「개입 N회」가 사람이 스스로 물어본 횟수까지 세게 된다 — 待った를
-- 갈라 둔 것과 같은 이유다. `taken`(알려줘도 못 찾았나)은 여기 칸으로 옮겨 온다.
CREATE TABLE game_hints (
    id      bigserial PRIMARY KEY,
    game_id bigint    NOT NULL REFERENCES games ON DELETE CASCADE,
    -- 물어본 자리의 手数. 같은 手数가 두 번 올 수 있다 — 단계가 둘이라서다(stage).
    ply int NOT NULL,
    -- 그 국면의 SFEN. **手数가 아니라 이것이 「같은 국면」의 자다** — 待った나 롤백으로
    -- 되돌아온 자리는 手数가 같아도 다시 물어볼 수 있어야 하고, 반대로 다른 手数에서
    -- 같은 국면에 이르면 그건 이미 답을 본 국면이다.
    sfen_key text NOT NULL,
    -- 1이면 「어느 駒인가」까지, 2면 「어떻게 움직이나」까지. 3은 없다 — 서버가 막는다.
    stage int NOT NULL,
    -- 알려준 수. **2단계에서만 화면에 나가지만 1단계에서도 적는다** — 무엇을 알려주려
    -- 했는지가 남아야 나중에 「알려줘도 못 찾았나」를 셀 수 있다(taken).
    best_usi text NOT NULL,
    -- 그 뒤에 사람이 실제로 그 수를 뒀는가. 물어본 시점에는 모르므로 **나중에 채운다.**
    -- NULL 은 「아직 안 뒀다」이고 false 와 다르다.
    taken      boolean,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_hints_game_idx ON game_hints (game_id, ply);
