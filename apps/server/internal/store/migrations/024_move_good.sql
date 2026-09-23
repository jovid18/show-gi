-- 사람이 둔 수가 好手였는지를 남긴다. 판정 기준은 journal §141.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **이미지보다 먼저 적용한다.** 이 파일 앞의 이미지는 두 칸을 읽지 않으므로 먼저 적용해도
-- 깨지지 않는다. 뒤의 이미지는 칸이 없으면 되짚기의 기보 읽기와 분석 기록이 실패한다.
-- 되돌릴 때는 두 칸을 DROP 하면 된다.

BEGIN;

-- 기본값이 false 라 이 파일 앞의 판은 전부 「好手 없음」으로 읽힌다. 그때는 판정 자체가
-- 없었으므로 되짚기에 점이 찍히지 않는 것이 맞다.
ALTER TABLE game_moves ADD COLUMN good boolean NOT NULL DEFAULT false;

-- 미리 잰 手의 판정 결과다. 판이 끝날 때 이 값을 game_moves 로 옮긴다(server 의 matchAnalyzer).
ALTER TABLE analysis_plies ADD COLUMN good boolean NOT NULL DEFAULT false;

COMMIT;
