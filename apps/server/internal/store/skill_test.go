package store

import "testing"

// 추정치가 판 사이로 넘어가는지. **이것이 안 되면 §47의 조절이 매 판 기준선에서 다시
// 시작하고**, 두 번째 판의 초심자가 첫 판과 똑같이 센 상대를 만난다.
func TestSkillEstimateRoundTrips(t *testing.T) {
	s := open(t)
	uid := owner(t, s, "skill")

	// 처음에는 행이 없다. **「모른다」와 「낙폭 0」이 갈려야 한다** — 0은 매 수 최선이라
	// 뜻이 정반대이고, 그걸 메우면 처음 로그인한 사람이 가장 센 상대를 만난다.
	if _, ok, err := s.SkillProfile(t.Context(), uid); err != nil || ok {
		t.Fatalf("빈 프로파일: ok = %v, err = %v", ok, err)
	}

	want := SkillEstimate{Loss: 0.42, Samples: 7}
	if err := s.SaveSkillEstimate(t.Context(), uid, want); err != nil {
		t.Fatalf("SaveSkillEstimate: %v", err)
	}
	got, ok, err := s.SkillProfile(t.Context(), uid)
	if err != nil || !ok {
		t.Fatalf("SkillProfile: ok = %v, err = %v", ok, err)
	}
	if got != want {
		t.Errorf("%+v, want %+v", got, want)
	}

	// 두 번째 저장은 **덮는다.** 행이 쌓이면 어느 것이 지금 값인지가 없어진다.
	next := SkillEstimate{Loss: 0.11, Samples: 19}
	if err := s.SaveSkillEstimate(t.Context(), uid, next); err != nil {
		t.Fatalf("두 번째 SaveSkillEstimate: %v", err)
	}
	got, _, err = s.SkillProfile(t.Context(), uid)
	if err != nil {
		t.Fatalf("SkillProfile: %v", err)
	}
	if got != next {
		t.Errorf("%+v, want %+v", got, next)
	}

	// 0도 저장돼야 한다 — 매 수 최선으로 두는 사람의 값이고, NULL로 떨어지면
	// 그 사람만 영원히 기준선에서 시작한다.
	if err := s.SaveSkillEstimate(t.Context(), uid, SkillEstimate{Loss: 0, Samples: 5}); err != nil {
		t.Fatalf("0 저장: %v", err)
	}
	got, ok, err = s.SkillProfile(t.Context(), uid)
	if err != nil || !ok {
		t.Fatalf("0 조회: ok = %v, err = %v", ok, err)
	}
	if got.Loss != 0 || got.Samples != 5 {
		t.Errorf("%+v, want {0 5}", got)
	}
}
