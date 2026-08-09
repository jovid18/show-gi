-- 로그인 전에도 대국을 남긴다.
--
-- Google 로그인은 D5인데 `games.user_id` 가 NOT NULL이라 그때까지는 한 판도 못 남긴다.
-- 그동안 둔 판을 버리면 **실력 추정의 원본이 통째로 사라진다** — 특히 개입으로 물러진
-- 「원래 두려던 수」는 개입에 오염되지 않은 유일한 신호라 나중에 다시 만들 수 없다
-- (docs/01-core.md §5).
--
-- 로그인이 붙으면 그때부터 채워진다. `skill_profile` 이 user_id를 요구하므로 익명 대국은
-- 아직 프로파일로 가지 않고 원본으로만 쌓인다.
ALTER TABLE games ALTER COLUMN user_id DROP NOT NULL;

-- 시작 국면을 그대로 둔다.
--
-- `root_key` 는 `positions` 참조라 캐시에 그 국면이 없으면 못 적는다. 그런데 기보만으로는
-- 어디서 시작했는지 알 수 없어서 **기록이 그것만으로 완결되지 않는다.** 중반 국면에서
-- 시작하는 대국이 실제로 있다(리뷰 화면·테스트).
ALTER TABLE games ADD COLUMN IF NOT EXISTS start_sfen text;
