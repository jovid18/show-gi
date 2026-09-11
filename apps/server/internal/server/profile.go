package server

import (
	"log"
	"net/http"
	"sort"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 마이페이지. 판을 가로질러 보는 하나뿐인 화면이다. 되짚기는 한 판을 열고 총평은 한 판을
// 세지만, 여기는 「지금까지 어땠나」에 답한다.
//
// 세는 것은 기록이고 段級만 추정기에서 온다(journal §62).

// weaknessMin 은 약점이라고 부르기 전에 필요한 개입 횟수다.
//
// 한 판에서 한 번 나온 카테고리까지 올리면 「당신의 약점」이 그날 우연히 둔 수 하나가 된다.
//
// [미확정] 회차 2가 8건뿐이라 표본 없이 정한 값이다.
const weaknessMin = 2

// weaknessTop 은 화면에 그리는 줄 수다. 아홉 종류를 다 그리면 목록이 「무엇이 약한가」
// 대신 「무엇에 걸렸나」가 된다.
const weaknessTop = 3

type profileHandler struct {
	store *store.Store
	auth  *authHandler
}

// weaknessView 는 약점 한 줄이다.
type weaknessView struct {
	Code string `json:"code"`
	// NameJa 는 개입 카드·총평과 같은 어휘다(explain.CategoryJa). 화면이 코드를
	// 일본어로 바꾸기 시작하면 어휘가 세 벌이 된다.
	NameJa string `json:"nameJa"`
	Count  int    `json:"count"`
	// Share 는 이 사람의 전체 개입 중 비율(0~1)이다. 12번이 많은지 적은지는 전체를 알아야
	// 답이 나온다.
	Share float64 `json:"share"`
}

type recordView struct {
	Games int `json:"games"`
	Win   int `json:"win"`
	Loss  int `json:"loss"`
	Draw  int `json:"draw"`
}

type profilePayload struct {
	Name string `json:"name"`
	// Rank 는 지금의 段級이다. 표본이 모자라면 이름을 붙이지 않고(skill.RankOf) 그때
	// 화면이 「まだ測っていません」을 그린다.
	Rank   *rankView  `json:"rank,omitempty"`
	Record recordView `json:"record"`
	// Interventions 는 전체 개입 횟수다. Weaknesses 의 Share 가 이 값에 대한 비율이라 같이
	// 보낸다. 화면이 목록을 더해 구하면 잘린 뒤의 합이라 틀린 분모가 된다.
	Interventions int            `json:"interventions"`
	Weaknesses    []weaknessView `json:"weaknesses,omitempty"`
	// Styles 는 지금까지 짠 囲い·전법·戦型이다. 약점과 같은 모집단에서 나온다
	// (store.PlayerTally).
	Styles []styleView `json:"styles,omitempty"`
}

// styleView 는 짠 진형 한 줄이다.
//
// 약점과 모양이 다르다. 저쪽은 비율이 뜻을 갖지만(전체 개입 중 몇 %가 이것인가) 이쪽은
// 「몇 판에서 짰나」라 분모가 판 수이고, 그 판 수는 이미 전적에 나와 있다.
type styleView struct {
	Code string `json:"code"`
	// NameJa 는 판에 뜨는 이름과 같은 어휘다(tag.Tag.NameJa). 화면이 코드를 일본어로
	// 바꾸기 시작하면 대국 중의 이름과 여기가 갈린다.
	NameJa string `json:"nameJa"`
	// Kind 는 축의 코드다(castle·formation·opening). 셋을 한 목록에 넣으므로 이것이
	// 없으면 「美濃囲い」와 「中飛車」가 같은 종류로 읽힌다.
	//
	// 일본어로 바꾸지 않는다. 축의 이름은 화면이 이미 갖고 있다(libs/game/tags.ts).
	// 이름(NameJa)이 반대인 것은 그쪽 어휘의 주인이 internal/tag 이기 때문이다.
	Kind  string `json:"kind"`
	Games int    `json:"games"`
}

// get 은 로그인한 사람의 프로파일 하나다.
//
// 익명에게는 주지 않는다. 익명 판은 서로 구별할 수단이 없어서(002_anonymous_games.sql)
// 「이 사람의 전적」이 그 배포에서 익명으로 둔 모든 사람의 전적이 된다.
func (h *profileHandler) get(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "ログインが必要です。",
		})
		return
	}

	tally, err := h.store.PlayerTally(r.Context(), &s.UserID)
	if err != nil {
		log.Printf("profile: tally for %d: %v", s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "成績を読み込めませんでした。",
		})
		return
	}

	out := profilePayload{Name: s.Name, Record: winLossDraw(tally.Results)}
	out.Interventions, out.Weaknesses = weaknessesOf(tally.Categories)
	out.Styles = stylesOf(tally.StyleTags)

	// 읽지 못해도 나머지는 준다. 段級이 없는 것은 「아직 재지 않았다」와 같은 화면이다
	// (Options.Store 와 같은 판단).
	if got, ok, err := h.store.SkillProfile(r.Context(), s.UserID); err != nil {
		log.Printf("profile: skill for %d: %v", s.UserID, err)
	} else if ok {
		if rank, named := skill.RankOf(skillEstimateOf(got)); named {
			out.Rank = &rankView{Step: rank.Step, Max: skill.RankMax, NameJa: rank.NameJa}
		}
	}

	writeJSON(w, http.StatusOK, out)
}

func winLossDraw(results map[store.GameResult]int) recordView {
	rec := recordView{
		Win:  results[store.ResultWin],
		Loss: results[store.ResultLoss],
		Draw: results[store.ResultDraw],
	}
	rec.Games = rec.Win + rec.Loss + rec.Draw
	return rec
}

// weaknessesOf 는 카테고리별 개입 횟수를 화면의 목록으로 바꾼다. 첫 값은 자르기 전의 전체
// 횟수다. Share 의 분모라 잘린 뒤의 합을 쓰면 비율이 1을 넘는다.
//
// 순서는 총평과 같은 규칙이다(summary.go 의 rankCategories). 무작위면 새로고침할 때마다
// 「당신의 약점 1위」가 바뀐다.
func weaknessesOf(counts map[string]int) (int, []weaknessView) {
	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return 0, nil
	}

	out := make([]weaknessView, 0, len(counts))
	for code, n := range counts {
		if n < weaknessMin {
			continue
		}
		out = append(out, weaknessView{
			Code:   code,
			NameJa: explain.CategoryJa(intervene.Category(code)),
			Count:  n,
			Share:  float64(n) / float64(total),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	if len(out) > weaknessTop {
		out = out[:weaknessTop]
	}
	return total, out
}

// styleTop 은 화면에 그리는 줄 수다. weaknessTop 보다 넉넉하다. 축이 셋이라 3줄이면
// 한 축 전체가 빠진다.
const styleTop = 8

// stylesOf 는 이름별 판 수를 화면의 목록으로 바꾼다.
//
// 모르는 코드는 버린다. 정의를 지운 뒤에도 옛 기록에는 그 코드가 남아 있고, 이름을
// 찾지 못한 것을 코드째로 내보내면 일본어 화면에 영어가 뜬다(tag.ByCode).
//
// 순서는 약점과 같은 규칙이다. 무작위면 새로고침마다 목록이 뒤집힌다.
func stylesOf(counts map[string]int) []styleView {
	out := make([]styleView, 0, len(counts))
	for code, n := range counts {
		t, ok := tag.ByCode(code)
		if !ok {
			continue
		}
		out = append(out, styleView{Code: code, NameJa: t.NameJa, Kind: string(t.Kind), Games: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Games != out[j].Games {
			return out[i].Games > out[j].Games
		}
		return out[i].Code < out[j].Code
	})
	if len(out) > styleTop {
		out = out[:styleTop]
	}
	return out
}
