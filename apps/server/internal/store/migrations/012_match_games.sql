-- 사람 대 사람 대국(초대 링크 방)의 기록. 정한 것과 그 근거는 journal §83.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 있다.** 컬럼 하나와 인덱스 하나를 더할 뿐이고, 이 파일 앞의 서버 이미지는
-- 이 칸을 안 읽는다 — 순서를 어느 쪽으로 잡아도 안 깨진다.

BEGIN;

-- 대인전 한 판은 `games` 행 **두 개**로 남는다 — 양쪽이 각자 자기 판을 갖는다.
--
-- **한 행으로 두지 않은 이유가 이 컬럼이 있는 이유이기도 하다.** 소유 검사를 타는 질의가
-- 다섯인데(ListGamesForOwner·GetGameForOwner·CountGameResultsForOwner·
-- CountInterventionCategoriesForOwner·CountGameStyleTagsForOwner) 행이 각자 것이면 그 다섯이
-- 한 줄도 안 바뀐다. 대신 두 행이 같은 판이라는 사실을 적을 자리가 없어져서, 그것을 여기가 맡는다.
--
-- 값은 방 id 그대로다(`internal/match` 의 128비트 난수). **어느 API도 이 칸을 안 돌려준다** —
-- 밖으로 나가는 것은 「대인전인가」라는 불리언 하나뿐이다(server/review.go 의 `isMatch`).
-- 그 문자열이 곧 초대 링크의 열쇠라서 그렇고, 덤으로 방이 판보다 먼저 닫히므로
-- (match.FinishedTTL) 여기 남은 값으로 열 수 있는 방은 없다.
--
-- NULL 이 「AI 연습 대국」이다. 마이페이지의 세 집계가 그 조건으로 대인전을 뺀다 —
-- 개입이 없는 판이 분모에 들어가면 「崩れやすいところ」의 비율이 그만큼 희석된다.
ALTER TABLE games ADD COLUMN IF NOT EXISTS match_id text;

-- 부분 인덱스다. **AI 대국이 행의 대부분이고 그쪽은 이 인덱스로 찾을 일이 없다.**
CREATE INDEX IF NOT EXISTS games_match_idx ON games (match_id) WHERE match_id IS NOT NULL;

COMMIT;
