-- 실력 추정치를 판 사이로 옮긴다. journal §47이 남긴 「skill_profile 이 비어 있다」.
--
-- **`rating_est`·`rating_sd` 를 쓰지 않는다.** §47이 레이팅 점수를 만들지 않기로 정했고
-- (낙폭은 임계치에 대한 비율이라야 뜻이 하나다) 그 칸에 비율을 넣으면 이름이 거짓말을 한다.
-- 001_init.sql 이 만들어 둔 두 칸은 손대지 않고 남긴다 — 지우는 것은 되돌릴 수 없고,
-- 병렬로 도는 다른 서버가 그 칸을 읽고 있을 수 있다(CLAUDE.md 의 워크트리 규약).
--
-- 칸 추가는 병렬 워크트리에서 안전하다. 모르는 서버는 그냥 안 읽는다.
ALTER TABLE skill_profile
    -- skill_loss 는 정규화된 낙폭(0~1). **NULL이 「아직 모른다」다** — 0은 「매 수 최선」이라
    -- 뜻이 정반대이고, 기본값을 0으로 두면 처음 로그인한 사람이 가장 센 상대를 만난다.
    ADD COLUMN IF NOT EXISTS skill_loss double precision,
    -- skill_samples 는 지금까지 본 판정 수의 누계. skill.MinSamples 를 넘겨야 밴드가 움직인다.
    ADD COLUMN IF NOT EXISTS skill_samples integer NOT NULL DEFAULT 0;
