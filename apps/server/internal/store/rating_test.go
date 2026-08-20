package store

import "testing"

// 레이팅이 판 사이로 넘어가는지, 그리고 「없다」가 「0점」과 갈리는지.
//
// 두 번째가 이 칸의 함정이다. rating_est 는 NOT NULL DEFAULT 0 이라 행만 있어도 값이
// 0으로 읽히는데, 레이팅에서 0은 「모른다」가 아니라 「아주 약하다」다 —
// Games 가 그 둘을 가른다(013_match_rating.sql).
func TestMatchRatingRoundTrips(t *testing.T) {
	s := open(t)
	a := owner(t, s, "a")
	b := owner(t, s, "b")

	// 행이 아예 없다.
	got, err := s.MatchRating(t.Context(), a)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if got.Games != 0 || got.SkillKnown {
		t.Fatalf("빈 프로파일: %+v", got)
	}

	// 엔진 대국만 한 사람. 행은 생기지만 레이팅은 아직 없다 — 시드를 만들 재료만 있다.
	if err := s.SaveSkillEstimate(t.Context(), a, SkillEstimate{Loss: 0.3, Samples: 9}); err != nil {
		t.Fatalf("SaveSkillEstimate: %v", err)
	}
	got, err = s.MatchRating(t.Context(), a)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if got.Games != 0 {
		t.Errorf("판이 %d, want 0 — 엔진 대국은 레이팅을 안 움직인다", got.Games)
	}
	if !got.SkillKnown || got.Skill.Loss != 0.3 || got.Skill.Samples != 9 {
		t.Errorf("시드 재료 %+v, want {0.3 9}", got.Skill)
	}

	// 한 판 반영. 두 사람이 같이 움직이고 판 수가 는다.
	wantA := MatchRating{Value: 1540, Deviation: 290}
	wantB := MatchRating{Value: 1460, Deviation: 290}
	if err := s.SaveMatchRatings(t.Context(), a, wantA, b, wantB); err != nil {
		t.Fatalf("SaveMatchRatings: %v", err)
	}

	for _, c := range []struct {
		name string
		id   int64
		want MatchRating
	}{{"a", a, wantA}, {"b", b, wantB}} {
		got, err := s.MatchRating(t.Context(), c.id)
		if err != nil {
			t.Fatalf("MatchRating(%s): %v", c.name, err)
		}
		if got.Value != c.want.Value || got.Deviation != c.want.Deviation {
			t.Errorf("%s: %.1f/%.1f, want %.1f/%.1f",
				c.name, got.Value, got.Deviation, c.want.Value, c.want.Deviation)
		}
		if got.Games != 1 {
			t.Errorf("%s: 판이 %d, want 1", c.name, got.Games)
		}
		if got.UpdatedAt.IsZero() {
			t.Errorf("%s: 갱신 시각이 비어 있다 — 안 둔 시간을 셀 수 없다", c.name)
		}
	}

	// 엔진 대국의 추정치가 안 지워졌다. 같은 행의 다른 칸이라, 한쪽 저장이 다른 쪽을
	// 덮으면 대인전 한 판이 그 사람의 개입 임계치를 기준선으로 되돌린다.
	got, err = s.MatchRating(t.Context(), a)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if !got.SkillKnown || got.Skill.Samples != 9 {
		t.Errorf("추정치가 %+v 로 바뀌었다, want {0.3 9}", got.Skill)
	}

	// 두 번째 판은 덮고 세기만 한다.
	if err := s.SaveMatchRatings(t.Context(), a, MatchRating{Value: 1555, Deviation: 270}, b, wantB); err != nil {
		t.Fatalf("두 번째 SaveMatchRatings: %v", err)
	}
	got, err = s.MatchRating(t.Context(), a)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if got.Value != 1555 || got.Games != 2 {
		t.Errorf("%.1f / %d 판, want 1555 / 2", got.Value, got.Games)
	}
}

// 한 사람을 두 자리에 넣으면 거절한다. 질의까지 가면 ON CONFLICT 가 같은 행을 두 번
// 고치려다 실패하고, 그 판은 반영되지 않은 채 에러 한 줄만 남는다.
func TestSaveMatchRatingsRefusesTheSameUserTwice(t *testing.T) {
	s := open(t)
	a := owner(t, s, "a")

	err := s.SaveMatchRatings(t.Context(), a, MatchRating{Value: 1500, Deviation: 300}, a, MatchRating{Value: 1500, Deviation: 300})
	if err == nil {
		t.Fatal("같은 사람 둘을 받았다")
	}
}
