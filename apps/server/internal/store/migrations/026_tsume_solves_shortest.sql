-- 기록 한 줄에 최단을 확인했는지를 남긴다. 목록이 최단을 확인하지 못한 詰み를 「N手詰め」로
-- 단정하지 않게 한다(journal §155).
--
-- 실행은 사람이 DB 클라이언트로 직접 한다 (deploy/README.md §4).
--
-- 서버 이미지보다 먼저 적용한다. 이 파일 앞의 이미지는 칸을 읽지 않는다. 뒤의 이미지는 칸이
-- 없으면 목록이 500 이고 기록 쓰기가 로그만 남긴다. 되돌릴 때는 칸을 DROP 하면 된다.

BEGIN;

-- 기본값이 false 라 확인하지 못한 쪽으로 읽힌다. 틀리게 단정하는 것보다 덜 말하는 쪽이 낫다.
ALTER TABLE tsume_solves ADD COLUMN shortest boolean NOT NULL DEFAULT false;

-- 이미 남은 줄은 캐시에 같은 답이 있으면 그 값을 옮긴다. solver 1 은 이 파일을 쓸 때의
-- tsume.Version 이다. 캐시에 없는 줄은 다시 열면(다시 풀면) 채워진다.
UPDATE tsume_solves s
SET shortest = r.shortest
FROM tsume_results r
WHERE r.sfen_key = s.sfen_key
  AND r.solver = 1
  AND s.status = 'mate'
  AND r.plies = s.plies;

COMMIT;
