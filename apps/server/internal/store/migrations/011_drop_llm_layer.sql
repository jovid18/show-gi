-- LLM 표현 계층을 걷어낸다. 개입 문구와 대국 총평은 이제 `internal/explain` 의 결정적
-- 문구 하나뿐이라, 여기 있는 것 전부가 부르는 코드가 없는 스키마다.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 없다.** `kb_chunks` 의 코퍼스와 `explain_cache` 에 쌓인 문장이 같이 사라지고,
-- 그 둘은 다시 만들 방법이 레포에 남지 않는다(003·004 는 이 파일이 대체한다). 이 마이그레이션
-- 뒤에 옛 서버 이미지를 되돌리면 그 서버는 없는 표를 찾다가 개입마다 로그를 남긴다 —
-- **앱 배포가 먼저고 이 파일이 나중이다.**

BEGIN;

-- 개입 하나에 든 계층과 돈. 부를 모델이 없으므로 늘 NULL·0으로만 쌓인다.
--
-- **`interventions` 의 나머지 칸은 안 건드린다.** 저 표는 「그 국면이 그 사람에게 얼마나
-- 어려웠나」의 기록이고 실력 추정이 그 위에서 돈다 — LLM과 무관하게 남는다.
ALTER TABLE interventions
    DROP COLUMN IF EXISTS explain_tier,
    DROP COLUMN IF EXISTS cost_yen;

-- Tier 0 캐시. 문장이 카테고리에서 결정적으로 나오므로 캐시할 것이 없다 —
-- 같은 사실에 같은 문장인 것은 이제 코드의 성질이다(explain.Render).
DROP TABLE IF EXISTS explain_cache;

-- RAG 코퍼스. 프롬프트가 없으면 붙일 자리가 없다.
DROP TABLE IF EXISTS kb_chunks;

-- **`vector` 확장은 안 지운다.** `kb_chunks.embedding` 이 유일한 사용처였지만, 확장을
-- 지우는 것은 데이터베이스 전체에 걸리는 일이고 되돌리려면 superuser 가 필요하다 —
-- 남겨 두는 비용은 카탈로그 몇 행이다.

COMMIT;
