-- 사람끼리 둔 판의 레이팅. 정한 것과 그 근거는 journal §92.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 있다.** 칸 둘을 더할 뿐이고, 이 파일 앞의 서버 이미지는 그 칸을 안 읽는다 —
-- 순서를 어느 쪽으로 잡아도 안 깨지고, 병렬 워크트리에서도 안전하다(CLAUDE.md).

BEGIN;

-- rating_est·rating_sd 는 001_init.sql 이 이미 만들어 뒀고 지금까지 아무도 안 썼다.
-- 006_skill_profile_estimate.sql 이 그 칸을 비워 두기로 정했고, 이 파일이 그것을 채운다.
--
-- §47의 결정은 그대로다. 저쪽이 정한 것은 「낙폭을 점수로 바꾸지 않는다」이고
-- skill_loss 는 여전히 임계치에 대한 비율이다 — 여기 들어가는 것은 그 옆의 다른 값이고,
-- 입력이 승패라서 척도가 다르다.
--
-- 두 칸을 손대지 않는 이유는 손댈 것이 없기 때문이다. rating_sd 의 기본값 350이 마침
-- Glicko 의 「전혀 모른다」이고(rating.MaxDeviation), rating_est 의 기본값 0은 아래
-- rating_games 가 0인 동안 아무도 안 읽는다.
ALTER TABLE skill_profile
    -- rating_games 는 레이팅에 반영된 대인전 판 수다. **0이 「아직 레이팅이 없다」다.**
    --
    -- rating_est 로 그것을 가릴 수 없어서 칸이 하나 필요했다 — 저쪽은 NOT NULL DEFAULT 0
    -- 이고, 레이팅에서 0은 「모른다」가 아니라 「아주 약하다」다. 006 이 skill_loss 에서
    -- NULL 과 0을 가른 것과 같은 함정이다.
    --
    -- 0인 사람에게는 엔진 대국의 추정치로 시드를 만든다(rating.SeedFromLoss).
    ADD COLUMN IF NOT EXISTS rating_games integer NOT NULL DEFAULT 0,
    -- rating_updated_at 은 레이팅이 마지막으로 움직인 시각이다. 안 둔 시간만큼 RD 를
    -- 되돌리는 데 쓴다(rating.Inflate).
    --
    -- updated_at 을 못 쓴다. 그 칸은 판정 한 건마다 갱신되므로(query/skill.sql 의
    -- SaveSkillEstimate) 「마지막으로 둔 대인전」이 아니라 「마지막으로 둔 수」다.
    ADD COLUMN IF NOT EXISTS rating_updated_at timestamptz;

COMMIT;
