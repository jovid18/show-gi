-- 사람이 밖에서 둔 자기 기보를 취해 오는 자리. 정한 것과 그 근거는 journal §126.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** (deploy/README.md §4).
--
-- **되돌릴 수 있고 순서를 어느 쪽으로 잡아도 안 깨진다.** 더하는 것이 nullable 컬럼 셋이라
-- 이 파일 앞의 이미지는 이 칸들을 안 읽고, 뒤의 이미지는 표가 없으면 취해 오기만 못 한다 —
-- 대국도 대인전 분석도 그대로 돈다.

BEGIN;

-- 취해 온 기보라는 사실과, 무엇으로 읽었는가를 한 칸이 같이 든다.
--   'kif' | 'ki2' | 'csa' | 'usi' | 'plain' | 'llm'
--
-- NULL 이 「여기서 둔 판」이다. 그래서 이 칸을 안 읽는 질의는 지금까지와 같은 답을 준다.
--
-- source 같은 칸을 안 만든다. 「대인전인가」는 match_id 가 이미 말하므로(012_match_games.sql)
-- 갈래 이름을 따로 두면 같은 사실이 두 벌이 되고 한쪽이 낡는다. 이 칸이 드는 것은 아무도
-- 안 들고 있는 사실 하나다.
--
-- **어느 API도 이 칸의 값을 그대로 안 돌려준다.** 밖으로 나가는 것은 「취해 온 판인가」라는
-- 불리언 하나다(server/review.go 의 imported) — 어느 파서가 읽었는지는 사람이 고칠 수 있는
-- 것이 아니고, 'llm' 이라는 값은 미리보기 화면에서 한 번 말하고 끝난다.
ALTER TABLE games ADD COLUMN IF NOT EXISTS imported_from text;

-- 부분 인덱스다. 여기서 둔 판이 행의 대부분이고 그쪽은 이 인덱스로 찾을 일이 없다
-- (games_match_idx 와 같은 판단). 하루 몫을 세는 질의가 이 인덱스를 탄다.
CREATE INDEX IF NOT EXISTS games_imported_idx ON games (user_id, started_at)
    WHERE imported_from IS NOT NULL;

-- 미리 잰 手가 판정 결과까지 들고 있어야 취해 온 판의 悪手 줄이 만들어진다.
--
-- 대인전은 이 두 칸이 언제나 NULL 이다 — 개입이 없는 판이라 카테고리를 쓸 자리가 없고,
-- 그쪽 경로는 이 칸을 안 읽는다(server 의 judged).
ALTER TABLE analysis_plies ADD COLUMN IF NOT EXISTS category text;
ALTER TABLE analysis_plies ADD COLUMN IF NOT EXISTS best_cp  int;

COMMIT;
