-- 실력 추정치의 절대 낙폭. 정한 것과 그 근거는 journal §94.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 있다.** 칸 둘을 더할 뿐이고, 이 파일 앞의 서버 이미지는 그 칸을 안 읽는다 —
-- 순서를 어느 쪽으로 잡아도 안 깨지고, 병렬 워크트리에서도 안전하다(CLAUDE.md).

BEGIN;

-- skill_loss 는 그대로 둔다. 저쪽은 임계치로 나눈 비율이고 상대의 강함이 그 값을 본다
-- (game.strengthStep) — 여기 들어가는 것은 그 옆의 다른 값이다.
--
-- 척도를 실측으로 보정하려면 앵커가 절대값 위에 있어야 한다. 비율 위에 잡으면 임계치가
-- 좁아지는 날 같은 실력이 네 계급 움직이고(journal §92의 표) 그때 앵커가 통째로 낡는다.
ALTER TABLE skill_profile
    -- skill_abs_loss 는 임계치로 나누지 않은 승률 낙폭의 누적 평균이다. **NULL이 「아직 모른다」다** —
    -- 006 이 skill_loss 에서 NULL 과 0을 가른 것과 같은 이유이고, 0은 「매 수 최선」이라 뜻이 정반대다.
    ADD COLUMN IF NOT EXISTS skill_abs_loss double precision,
    -- skill_abs_samples 는 그 평균에 들어간 수의 개수다. skill_samples 로 대신할 수 없다 —
    -- 이 파일 전에 쌓인 행은 저쪽이 차 있는데 낙폭의 합이 없어서, 평균을 그 개수로 나누면
    -- 없는 값을 0으로 세는 것이 된다(skill.NewTrackFrom).
    ADD COLUMN IF NOT EXISTS skill_abs_samples integer NOT NULL DEFAULT 0;

COMMIT;
