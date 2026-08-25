package main

import "testing"

// 티어 이름을 잘못 적으면 both 로 떨어진다. 조용히 떨어지면 안 되는 자리다 — 분석
// 티어를 띄웠다고 믿는데 상호작용 티어도 같이 집으면 회차가 아무것도 안 가른다.
func TestTheRoleFallsBackToBoth(t *testing.T) {
	for _, tc := range []struct{ set, want string }{
		{"", roleBoth},
		{roleBoth, roleBoth},
		{roleInteractive, roleInteractive},
		{roleAnalysis, roleAnalysis},
		{"analyis", roleBoth},
		{"Analysis", roleBoth},
	} {
		t.Setenv("ROLE", tc.set)
		if got := analysisRole(); got != tc.want {
			t.Errorf("ROLE=%q 에서 %q, %q 여야 한다", tc.set, got, tc.want)
		}
	}
}

// 상호작용 티어는 집는 쪽을 안 띄운다. ANALYSIS_WORKERS 가 있어도 그렇다 — 손잡이가
// 둘이면 어느 쪽이 이기는지가 배포 로그에서만 갈린다.
func TestTheInteractiveTierTakesNoWorkers(t *testing.T) {
	t.Setenv("ROLE", roleInteractive)
	t.Setenv("ANALYSIS_WORKERS", "4")
	if got := analysisWorkers(2); got != 0 {
		t.Errorf("워커가 %d개다, 0이어야 한다", got)
	}

	t.Setenv("ROLE", roleAnalysis)
	if got := analysisWorkers(2); got != 4 {
		t.Errorf("워커가 %d개다, ANALYSIS_WORKERS 인 4여야 한다", got)
	}
}
