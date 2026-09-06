-- 詰み을 cp 로 눌러 담던 칸 셋에 태그를 준다. 정한 것과 그 근거는 journal §131.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 더하는 것이 nullable 컬럼 셋이고,
-- 이 파일 앞의 이미지는 그 칸을 안 읽는다. 뒤의 이미지는 칸이 없으면 평가치를 못 적지만
-- 대국 자체는 돈다 — 평가치는 언제나 늦게 오는 값이라 NULL 이 정상 상태다.

BEGIN;

-- 깊이별 값의 詰み 짝. eval_by_depth[i] 와 mate_by_depth[i] 중 하나만 값이 있다.
--
-- 배열을 둘로 나눈 것은 int[] 이 원소마다 NULL 을 들 수 있기 때문이다 — jsonb 로 바꾸면
-- 한 국면이 후보 k개 × 깊이 14 만큼의 객체가 되고, 이 칸은 판마다 매 手 쓰인다.
--
-- 길이는 항상 같다. 한쪽만 쓰면 그 깊이가 「cp 인지 詰み인지 모르는 값」이 되는데,
-- 그 상태를 표현하지 않기로 한 것이 이 마이그레이션의 전부다.
ALTER TABLE edges ADD COLUMN IF NOT EXISTS mate_by_depth int[];

-- 그 수 뒤의 詰み까지의 手数(先手 관점). eval_cp 와 배타적이다.
--
-- CHECK 가 배타를 든다. 「둘 다 NULL」은 남겨 둔다 — 평가치가 아직 안 온 手数가 그것이고,
-- 그 자리가 이 칸들의 정상 상태다(store.RecordedMove).
ALTER TABLE game_moves ADD COLUMN IF NOT EXISTS eval_mate int;
ALTER TABLE game_undos ADD COLUMN IF NOT EXISTS eval_mate int;

-- 대인전 분석이 미리 재 둔 값. 여기를 지나 game_moves 로 들어가므로 이 칸이 평평하면
-- 위의 태그가 그 경로에서만 다시 눌린다 — 걷히는 표라도 값이 지나가는 길은 같다.
ALTER TABLE analysis_plies ADD COLUMN IF NOT EXISTS before_mate int;
ALTER TABLE analysis_plies ADD COLUMN IF NOT EXISTS after_mate int;
ALTER TABLE analysis_plies ADD COLUMN IF NOT EXISTS best_mate int;

-- 판정의 두 원본. 이 칸이 있는 이유가 「K를 바꿔 다시 채점한다」인데
-- (005_intervention_cp.sql) 詰み을 cp로 눌러 적으면 그 값이 K와 무관해져서 다시 채점할
-- 것이 아니게 된다 — 눌린 숫자는 어떤 K에서도 같은 승률을 낸다.
ALTER TABLE interventions ADD COLUMN IF NOT EXISTS best_mate int;
ALTER TABLE interventions ADD COLUMN IF NOT EXISTS after_mate int;

ALTER TABLE interventions DROP CONSTRAINT IF EXISTS interventions_best_one_of;
ALTER TABLE interventions ADD CONSTRAINT interventions_best_one_of
    CHECK (best_cp IS NULL OR best_mate IS NULL);

ALTER TABLE interventions DROP CONSTRAINT IF EXISTS interventions_after_one_of;
ALTER TABLE interventions ADD CONSTRAINT interventions_after_one_of
    CHECK (after_cp IS NULL OR after_mate IS NULL);

ALTER TABLE game_moves DROP CONSTRAINT IF EXISTS game_moves_eval_one_of;
ALTER TABLE game_moves ADD CONSTRAINT game_moves_eval_one_of
    CHECK (eval_cp IS NULL OR eval_mate IS NULL);

ALTER TABLE game_undos DROP CONSTRAINT IF EXISTS game_undos_eval_one_of;
ALTER TABLE game_undos ADD CONSTRAINT game_undos_eval_one_of
    CHECK (eval_cp IS NULL OR eval_mate IS NULL);

-- 옛 값을 비운다. 여기 있던 詰み은 ±(30000 − 10×手数) 로 눌린 숫자이고, 같은 구간에
-- 엔진의 생 cp(±35281 — 「이기는데 手数를 모름」)가 섞여 있어 둘을 되돌려 가를 수 없다.
--
-- **버리는 것이 안전한 이유는 프로덕션에 행이 없기 때문이다** — 2026-09-04 에 인프라를
-- 통째로 내리면서 DB 를 스냅샷 없이 버렸다(journal §128). 남은 것은 개발용 DB 뿐이고,
-- 그쪽 값은 다시 재면 나온다. 값이 있는 DB 에 이 파일을 돌리는 날에는 이 문단을 먼저 읽는다.
UPDATE game_moves SET eval_cp = NULL WHERE abs(eval_cp) > 20000;
UPDATE game_undos SET eval_cp = NULL WHERE abs(eval_cp) > 20000;
UPDATE interventions SET best_cp = NULL WHERE abs(best_cp) > 20000;
UPDATE interventions SET after_cp = NULL WHERE abs(after_cp) > 20000;
-- 이미 잰 手도 비운다. 그 행은 판이 끝날 때 game_moves 로 옮겨 담기므로
-- (MeasuredAnalysisPlies → setEval) 안 비우면 방금 청소한 칸에 눌린 값이 다시 들어간다.
UPDATE analysis_plies SET before_cp = NULL WHERE abs(before_cp) > 20000;
UPDATE analysis_plies SET after_cp = NULL WHERE abs(after_cp) > 20000;
UPDATE analysis_plies SET best_cp = NULL WHERE abs(best_cp) > 20000;
UPDATE edges SET eval_by_depth = NULL
WHERE EXISTS (SELECT 1 FROM unnest(eval_by_depth) v WHERE abs(v) > 20000);

COMMIT;
