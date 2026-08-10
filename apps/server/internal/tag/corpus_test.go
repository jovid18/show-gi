package tag

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// 코퍼스 적재 마이그레이션. **DB를 안 타고 파일을 읽는다.**
//
// DB 테스트로 쓰면 `SHOWGI_TEST_DATABASE_URL` 이 없을 때 조용히 skip 되고 초록으로
// 보인다(CLAUDE.md). 코퍼스에 한국어가 섞이는 것은 그렇게 놓치면 안 되는 종류라 —
// 화면과 프롬프트로 그대로 새는데 에러가 안 난다 — 언제나 도는 자리에 둔다.
const corpusPath = "../store/migrations/003_kb_chunks_seed.sql"

func corpusSQL(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("코퍼스를 못 읽었다: %v", err)
	}
	return string(b)
}

// bodies 는 INSERT 의 본문 리터럴만 뽑는다. 주석에는 한국어가 있어야 하므로(설계 기록)
// 통째로 훑으면 안 된다.
//
// 본문은 「'…日本語…',」 꼴의 여러 줄 문자열 중 CJK가 들어 있는 것이다. 태그 배열과
// URL은 ASCII라 저절로 걸러진다.
func bodies(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, line := range strings.Split(corpusSQL(t), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "--") || !strings.HasPrefix(s, "'") {
			continue
		}
		if strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		t.Fatal("본문을 하나도 못 뽑았다 — 이 테스트가 아무것도 안 보고 있다")
	}
	return out
}

// **코퍼스는 일본어만이다.** 한국어가 섞이면 일본어 질의와 임베딩 유사도가 무너지고,
// 근거로 붙인 문장이 그대로 출력으로 샌다(CLAUDE.md 언어 규칙).
//
// 키릴 문자까지 같이 본다. 실제로 초안에 두 군데 섞여 있었고 — `серьез攻め` 였다 —
// 일본어 사이에 한두 글자 들어가면 사람 눈으로는 그냥 안 보인다.
func TestCorpusIsJapaneseOnly(t *testing.T) {
	for _, body := range bodies(t) {
		for _, r := range body {
			switch {
			case unicode.Is(unicode.Hangul, r):
				t.Errorf("코퍼스에 한글이 있다: %q", body)
			case unicode.Is(unicode.Cyrillic, r):
				t.Errorf("코퍼스에 키릴 문자가 있다: %q", body)
			}
		}
	}
}

// 태그는 있는데 해설이 없으면 화면에 이름만 뜨고 배울 것이 없다. 정의를 추가하고
// 코퍼스를 빠뜨리면 여기가 잡는다.
func TestEveryTagHasACorpusEntry(t *testing.T) {
	sql := corpusSQL(t)
	for _, tg := range All() {
		if !strings.Contains(sql, "'"+tg.Code+"'") {
			t.Errorf("%s (%s): 코퍼스 항목이 없다", tg.Code, tg.NameJa)
		}
		if !strings.Contains(sql, tg.NameJa) {
			t.Errorf("%s: 일본어 이름이 코퍼스에 안 나온다", tg.Code)
		}
	}
}

// 囲い 좌표의 출처가 코퍼스의 출처와 같아야 한다. 갈라지면 화면이 가리키는 근거와
// 프롬프트에 붙는 근거가 다른 문서가 된다.
func TestCastleSourcesAppearInTheCorpus(t *testing.T) {
	sql := corpusSQL(t)
	for _, sh := range castles {
		if !strings.Contains(sql, sh.source) {
			t.Errorf("%s: 좌표 출처(%s)가 코퍼스에 없다", sh.tag.Code, sh.source)
		}
	}
}

// `verified_by` 가 비면 검색에 걸려도 프롬프트에 안 붙는다(001_init.sql 부분 인덱스).
// 넣어놓고 안 쓰이는 행이 생기지 않게, 모든 행이 검증 표시를 갖는지 본다.
func TestEveryRowIsVerifiedAndSourced(t *testing.T) {
	sql := corpusSQL(t)

	rows := regexp.MustCompile(`(?m)^\s*ARRAY\[`).FindAllStringIndex(sql, -1)
	verified := strings.Count(sql, "'engine')")
	if len(rows) != verified {
		t.Errorf("행 %d개 중 verified_by 가 붙은 것이 %d개다", len(rows), verified)
	}

	// 라이선스는 스키마의 CHECK 가 아니라 규약이라 여기서 본다.
	for _, lic := range []string{"'CC-BY-SA-4.0'", "'engine-derived'"} {
		if !strings.Contains(sql, lic) {
			t.Errorf("쓰기로 한 라이선스 %s 가 안 보인다", lic)
		}
	}
	if strings.Contains(sql, "http://") {
		t.Error("출처 URL이 http 다 — 없어질 수 있는 주소를 근거로 박으면 안 된다")
	}
}
