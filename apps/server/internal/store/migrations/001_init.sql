-- show-gi 초기 스키마. 설계 근거는 docs/02-architecture.md §4.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다.
-- db 컨테이너는 docker-entrypoint-initdb.d 를 마운트하지 않는다(deploy/README.md §4).
-- 스키마를 바꿀 때는 이 파일이 아니라 다음 번호의 파일을 새로 추가한다.

CREATE EXTENSION IF NOT EXISTS vector;

-- ─── 사용자 ─────────────────────────────────────────────────

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    provider      text        NOT NULL,          -- 'google'
    provider_uid  text        NOT NULL,
    display_name  text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_uid)
);

-- 실력 프로파일. 본인만 조회할 수 있어야 한다 (docs/02-architecture.md §7 위협 2).
CREATE TABLE skill_profile (
    user_id    bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    rating_est double precision NOT NULL DEFAULT 0,
    rating_sd  double precision NOT NULL DEFAULT 350,
    weakness   jsonb            NOT NULL DEFAULT '{}',  -- 카테고리별 발생률
    updated_at timestamptz      NOT NULL DEFAULT now()
);

-- ─── 국면 그래프 ────────────────────────────────────────────
-- 노드 = 국면, 간선 = 한 수. 그래프 DB를 쓰지 않는 이유는 §4에 있다 —
-- 필요한 질의가 1-hop뿐이고 가변 길이 경로 탐색이 없다.

CREATE TABLE positions (
    -- 手数를 뺀 SFEN. 手数를 빼야 전치(transposition)가 같은 행으로 합쳐진다
    sfen_key       text PRIMARY KEY,
    side_to_move   char(1) NOT NULL CHECK (side_to_move IN ('b', 'w')),
    ply_hint       int,
    candidates     jsonb,   -- MultiPV 상위 k: [{usi, cp, pv}]
    -- 더 얕게 계산한 결과로 깊은 결과를 덮어쓰지 않기 위한 값
    computed_depth int  NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE edges (
    parent_key text NOT NULL REFERENCES positions ON DELETE CASCADE,
    usi        text NOT NULL,
    child_key  text REFERENCES positions ON DELETE SET NULL,
    tags       text[] NOT NULL DEFAULT '{}',   -- 이 수로 성립한 囲い·전법·手筋
    -- [d1, d2, … d14] 선수(sente) 관점 cp.
    -- 개입 판정의 입력이다: 얕은 값과 깊은 값의 부호가 갈리면 함정이거나 手筋이다.
    eval_by_depth int[],
    PRIMARY KEY (parent_key, usi)
);

CREATE INDEX edges_tags_idx ON edges USING gin (tags);

-- ─── 대국 ───────────────────────────────────────────────────

CREATE TABLE games (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    my_color    char(1) NOT NULL CHECK (my_color IN ('b', 'w')),
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    result      text,          -- 'win' | 'loss' | 'draw' | 'abandoned'
    opening_tag text,          -- 컴퓨터가 고른 전법
    root_key    text REFERENCES positions
);

CREATE TABLE game_moves (
    game_id  bigint NOT NULL REFERENCES games ON DELETE CASCADE,
    ply      int    NOT NULL,
    usi      text   NOT NULL,
    sfen_key text REFERENCES positions,
    eval_cp  int,
    PRIMARY KEY (game_id, ply)
);

-- ─── 개입 ───────────────────────────────────────────────────
-- 제품의 코어. 방향이 둘이다 (docs/01-core.md §1).

CREATE TABLE interventions (
    id           bigserial PRIMARY KEY,
    game_id      bigint NOT NULL REFERENCES games ON DELETE CASCADE,
    ply          int    NOT NULL,
    -- 'blunder' = 제지형(착수 후 롤백) | 'tesuji' = 제안형(착수 전 알림)
    kind         text   NOT NULL CHECK (kind IN ('blunder', 'tesuji')),
    category     text,             -- 블런더 카테고리 또는 手筋 태그
    delta_win    double precision, -- 승률 낙폭(제지형) / 반전 폭(제안형)
    level_bucket text,

    -- 제지형만: 개입이 없었다면 두었을 수. 개입에 오염되지 않은 유일한 실력 신호다
    retracted_usi text,
    -- 제안형만: 알린 태그와, 플레이어가 실제로 그 수를 찾았는지
    hinted_tag   text,
    taken        boolean,

    explain_tier smallint,         -- 0=캐시 히트 1=소형 2=대형
    cost_yen     numeric(10, 4) NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),

    -- kind별로 채우는 컬럼이 다르다. 섞이면 실력 추정이 조용히 틀어지므로 DB에서 막는다
    CONSTRAINT intervention_fields_match_kind CHECK (
        (kind = 'blunder' AND hinted_tag IS NULL AND taken IS NULL)
        OR (kind = 'tesuji' AND retracted_usi IS NULL)
    )
);

CREATE INDEX interventions_game_idx ON interventions (game_id, ply);

-- ─── LLM ────────────────────────────────────────────────────

-- Tier 0. 카테고리가 유한하므로 설명의 대부분이 여기서 끝난다.
CREATE TABLE explain_cache (
    key        text PRIMARY KEY,   -- hash(kind, category, level_bucket, piece, 국면특징)
    body       text NOT NULL,      -- 일본어
    model      text,
    hits       int  NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- RAG 코퍼스. 출처가 없거나 검증되지 않은 chunk는 프롬프트에 붙이지 않는다 —
-- 그 규칙을 코드가 아니라 스키마에서 강제한다 (docs/09-tags.md §0).
CREATE TABLE kb_chunks (
    id             bigserial PRIMARY KEY,
    title          text NOT NULL,
    body           text NOT NULL,          -- 일본어. 한국어를 넣으면 검색이 무너진다
    tags           text[] NOT NULL DEFAULT '{}',
    source_url     text NOT NULL,
    source_license text NOT NULL,          -- 'CC-BY-SA-4.0' | 'official-reference' | 'engine-derived'
    verified_by    text CHECK (verified_by IN ('engine', 'human')),
    embedding      vector(1536),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX kb_chunks_tags_idx ON kb_chunks USING gin (tags);
-- 검증된 것만 검색 대상이다. 부분 인덱스라 미검증 행은 애초에 후보에 오르지 않는다
CREATE INDEX kb_chunks_verified_idx ON kb_chunks (id) WHERE verified_by IS NOT NULL;
