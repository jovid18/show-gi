package store

import (
	"encoding/json"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
)

// DB가 필요 없다. 행의 모양만 보는 시험이라 여기가 CI에서 실제로 도는 유일한 store 층이다.

// cp 와 mate 가 한 행에 같이 나가면 합성값이 다시 생긴다. 스키마가 그것을 막는다.
func TestACandidateWritesEitherCpOrMateNeverBoth(t *testing.T) {
	cp, err := json.Marshal(Candidate{USI: "7g7f", Score: eval.Cp(143)})
	if err != nil {
		t.Fatalf("cp 후보: %v", err)
	}
	if got, want := string(cp), `{"usi":"7g7f","cp":143}`; got != want {
		t.Errorf("cp 후보 = %s, want %s", got, want)
	}

	mate, err := json.Marshal(Candidate{USI: "2b3c", Score: eval.Mate(1)})
	if err != nil {
		t.Fatalf("詰み 후보: %v", err)
	}
	if got, want := string(mate), `{"usi":"2b3c","mate":1}`; got != want {
		t.Errorf("詰み 후보 = %s, want %s", got, want)
	}
}

// 옛 행은 詰み 줄에 cp 와 mate 를 함께 적었고 그 cp 는 환산값이다. mate 가 있으면
// 그쪽이 이긴다 — 그래서 마이그레이션이 없다(journal §131).
func TestAnOldRowKeepsItsMateAndDropsTheSynthesisedCp(t *testing.T) {
	var c Candidate
	if err := json.Unmarshal([]byte(`{"usi":"2b3c","cp":29990,"mate":1,"pv":["2b3c"]}`), &c); err != nil {
		t.Fatalf("옛 행: %v", err)
	}
	if c.Score != eval.Mate(1) {
		t.Errorf("점수 = %+v, want mate 1", c.Score)
	}
	if len(c.PV) != 1 || c.PV[0] != "2b3c" {
		t.Errorf("PV = %v", c.PV)
	}
}

func TestCandidateRoundTrips(t *testing.T) {
	for _, want := range []Candidate{
		{USI: "7g7f", Score: eval.Cp(0), PV: []string{"7g7f", "3c3d"}},
		{USI: "7g7f", Score: eval.Cp(-35281)},
		{USI: "2b3c", Score: eval.Mate(-3)},
		// mate 0 은 「이미 詰んでいる」이라 지는 쪽이다. omitempty 로 적으면 이 행이
		// cp 0 으로 돌아왔다 — 포인터로 내보내는 이유가 이 한 줄이다.
		{USI: "2b3c", Score: eval.Mate(0)},
	} {
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("%+v: %v", want, err)
		}
		var got Candidate
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		if got.USI != want.USI || got.Score != want.Score {
			t.Errorf("%s → %+v, want %+v", b, got, want)
		}
	}
}
