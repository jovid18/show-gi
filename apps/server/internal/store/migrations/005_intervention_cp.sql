-- 물러진 수의 원본 cp를 남긴다.
--
-- 낙폭(`delta_win`)은 두 승률의 **차**라 되돌릴 수 없다 — 판정이 손에 들고 있던 원본 cp를
-- 그대로 남긴다. **둘 다 nullable 이고 과거 행은 영원히 NULL 이다.** 실측과 배경은
-- 06-status.md §39.

BEGIN;

ALTER TABLE interventions
    ADD COLUMN best_cp  int,
    ADD COLUMN after_cp int;

COMMENT ON COLUMN interventions.best_cp IS
    '판정 당시 최선수의 cp(수번 측 관점). 제지형만. 과거 행은 NULL';
COMMENT ON COLUMN interventions.after_cp IS
    '물러진 수를 둔 뒤의 cp(수번 측 관점). 제지형만. 과거 행은 NULL';

COMMIT;
