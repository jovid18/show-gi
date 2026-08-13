package explain

import "testing"

// **회차 2 에서 실제로 나온 네 가지 표기다**(06-status.md §59). 8건 전부 정상 호출이었는데도
// 숫자가 이렇게 갈렸다 — 라우터 장애 탓이 아니었다.
func TestScoreNotationIsNormalized(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"形勢は-123です", "形勢は-123です"},
		{"マイナス601になります", "-601になります"},
		{"プラス323です", "+323です"},
		{"＋３２３になります", "+323になります"},
		{"−961です", "-961です"},
	} {
		if got := normalizeScores(tc.in); got != tc.want {
			t.Errorf("%q → %q, want %q", tc.in, got, tc.want)
		}
	}
}

// **長音符와 ダッシュ는 부호가 아니다.** 일본어 본문에 정상적으로 나타나는 글자라, 부호로
// 보고 바꾸면 문장을 망가뜨린다 — 이 테스트가 없으면 「レベル」류가 조용히 깨진다.
func TestNormalizeScoresLeavesJapaneseTextAlone(t *testing.T) {
	for _, s := range []string{
		"プレーヤーの持ち駒が増えます",
		"相手の利きに入っています——注意しましょう",
		"一手ずつ見ていきましょう",
	} {
		if got := normalizeScores(s); got != s {
			t.Errorf("본문을 건드렸다: %q → %q", s, got)
		}
	}
}

// **정규화가 검사보다 먼저다.** 「マイナス601」이 그대로 검사에 들어가면 표기가 통과해 버리고,
// 화면에는 갈린 문장이 그대로 나간다.
func TestCleanBranchesNormalizesBeforeChecking(t *testing.T) {
	got, ok := CleanBranches("▲2四歩 → 相手は△同歩 → マイナス601", []string{"▲2四歩", "△同歩"})
	if !ok {
		t.Fatal("정상 문장이 버려졌다")
	}
	if got != "▲2四歩 → 相手は△同歩 → -601" {
		t.Errorf("정규화가 안 됐다: %q", got)
	}
}
