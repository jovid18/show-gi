-- 실력 프로파일. 본인만 조회할 수 있어야 한다(02-architecture.md §7 위협 2) —
-- 여기 오는 user_id 는 언제나 서명 쿠키에서 나온 값이다(server/auth.go).

-- name: GetSkillProfile :one
--
-- 없으면 0행이다. 부르는 쪽이 그때 「아직 모른다」로 시작한다 — 행을 미리 만들지 않는 것은
-- 로그인만 하고 안 두는 사람에게 빈 행을 남기지 않기 위해서다.
SELECT skill_loss, skill_samples, skill_abs_loss, skill_abs_samples
FROM skill_profile
WHERE user_id = $1;

-- name: SaveSkillEstimate :exec
--
-- 판정 한 건마다 부른다. 대국이 끝날 때 한 번이 아니다 — 새로고침하면 판이 끝나므로
-- (journal §46) 끝에 몰아 쓰면 중간에 끊긴 판의 추정이 통째로 사라진다.
--
-- weakness 는 건드리지 않는다. 카테고리별 발생률은 아직 쓰는 쪽이 없다.
INSERT INTO skill_profile (user_id, skill_loss, skill_samples, skill_abs_loss, skill_abs_samples)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET skill_loss        = EXCLUDED.skill_loss,
    skill_samples     = EXCLUDED.skill_samples,
    skill_abs_loss    = EXCLUDED.skill_abs_loss,
    skill_abs_samples = EXCLUDED.skill_abs_samples,
    updated_at        = now();

-- name: SaveSkillEstimateIfSamples :execrows
--
-- 읽은 값이 그대로일 때만 덮는다. 대인전의 사후 분석이 쓰는 자리다(server/match_analysis.go) —
-- 저쪽은 판을 다 재는 동안(수십 초) 지난 값을 손에 들고 있어서, 그냥 덮으면 그 사이에
-- 끝난 엔진 대국의 판정을 통째로 지운다.
--
-- 진 쪽은 아무것도 안 쓴다. 0행을 돌려주므로 부르는 쪽이 다시 읽어 얹는다.
--
-- 행이 없으면 그대로 만든다. 그때 지울 것이 없고, 그 사이에 누가 만들었으면
-- ON CONFLICT 의 WHERE 가 막는다.
INSERT INTO skill_profile (user_id, skill_loss, skill_samples, skill_abs_loss, skill_abs_samples)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET skill_loss        = EXCLUDED.skill_loss,
    skill_samples     = EXCLUDED.skill_samples,
    skill_abs_loss    = EXCLUDED.skill_abs_loss,
    skill_abs_samples = EXCLUDED.skill_abs_samples,
    updated_at        = now()
WHERE skill_profile.skill_samples = sqlc.arg('expected_samples')::int;
