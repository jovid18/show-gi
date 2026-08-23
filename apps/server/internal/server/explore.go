package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 「検討」 — 手合割을 골라 0手目부터 아무 수나 둬 보면서 형세와 최선수 셋을 읽는 판.
//
// 계산부는 되짚기와 같다(branch.go 의 whatifNodeOf). 갈리는 것은 뿌리 하나뿐이고,
// 그 자리가 이제 셋이다 — 끝난 판은 DB 기록에서, 두는 중인 판은 세션 스냅샷에서, 여기는
// 手合割 표에서(internal/handicap) 뿌리를 얻는다(journal §85).
//
// 판(SFEN)은 여기서도 받지 않는다. 받는 것은 手合割 id와 수순뿐이고 서버가 매번 되짚어
// 한 수씩 룰 엔진에 검증시킨다 — §37이 닫아 둔 문("아무 국면이나 깊이 12로 재 주는 공개
// 엔진")이 그대로 닫혀 있는 것이 이 표면의 조건이다.
//
// 그래도 이 표면이 그 문에 가장 가깝다. 뿌리가 0手目라 합법적으로 도달하는 국면이면
// 무엇이든 물을 수 있다. 그래서 벽이 둘이다 — 로그인과 엔진 슬롯 하나.

const (
	// exploreMaxLine 은 검토 한 줄의 상한이다. 되짚기(whatifMaxLine=60)보다 긴 것이
	// 뿌리가 다르기 때문이다 — 저쪽은 그 手数까지가 이미 기록이고 그 뒤로만 뻗는데,
	// 여기는 0手目부터라 한 판을 통째로 걸어 볼 수 있어야 한다.
	exploreMaxLine = 200

	// exploreBodyLimit 은 본문 상한이다. 手合割 id 하나와 수순 한 줄이 전부다.
	exploreBodyLimit = 16 << 10

	// exploreSlots 는 이 표면이 동시에 잡을 수 있는 엔진 수다. 이것이 유일한 벽이다.
	//
	// 풀은 대국이 쓰는 것과 같은 3개다(main.go 의 defaultEnginePoolSize). 안 묶으면
	// 검토 세 건이 엔진을 다 잡고 대국의 착수가 그 뒤에 줄을 서므로, 개입 카드가 늦게 뜬다.
	// 1이면 대국에 언제나 2개가 남는다 — 올릴 자리가 여기 하나다.
	//
	// 로그인 벽이 여기 있었다(journal §100). 걷었으므로 이 수가 곧 「검토가 엔진에서
	// 가져갈 수 있는 전부」다.
	exploreSlots = 1

	// exploreWait 은 슬롯을 기다리는 시간이다. 처음 보는 국면 한 번이 ~1.5s이므로
	// (journal §37) 앞사람 하나가 끝나기를 기다릴 만큼이고, 그보다 밀리면 기다리게 두지 않고
	// 「まだ読んでいます」로 답한다 — 화면이 다시 누를 수 있는 실패다.
	exploreWait = 3 * time.Second
)

// exploreHandler 는 검토 판의 한 걸음을 답한다. 되짚기와 달리 DB에도 로그인에도 매여
// 있지 않다 — 뿌리가 기록이 아니라 상수 표라, 캐시(positions)는 있으면 쓰고 없으면 그냥
// 느리고, 열리는 기록이 없으니 자격을 물을 것도 없다.
type exploreHandler struct {
	// store 는 캐시로만 쓴다. nil이면 답은 같고 같은 국면을 매번 다시 잰다.
	store  *store.Store
	search Searcher
	// slots 는 이 표면의 유일한 벽이다. 빈자리가 없으면 exploreWait 만큼만 기다린다.
	slots chan struct{}
}

func newExploreHandler(st *store.Store, search Searcher) *exploreHandler {
	return &exploreHandler{
		store:  st,
		search: search,
		slots:  make(chan struct{}, exploreSlots),
	}
}

// exploreRequest 는 「이 手合割의 0手目에서 이 수순을 뒀다면」이다.
//
// 手数가 없다. 뿌리가 언제나 0手目라 whatifRequest.Ply 에 해당하는 값이 상수이고,
// 받으면 「기록의 몇 手目」이라는 뜻 없는 손잡이가 하나 생긴다.
type exploreRequest struct {
	// Handicap 은 手合割 id다. 빈 값이 平手다 — 화면의 기본값이고 표에 없다
	// (internal/handicap 의 규약).
	Handicap string `json:"handicap"`
	// Moves 는 양쪽 수가 전부 들어 있는 한 줄이다. 서버는 한 수도 대신 두지 않는다.
	Moves []string `json:"moves"`
}

// exploreNode 는 검토의 한 국면이다. 되짚기의 노드에 그 手合의 「형세 0」 두 칸을 얹는다.
//
// 화면이 그 값을 만들 수 없다. 二枚落ち의 0手目가 +1386인데(handicap.BaselineCp) 그것을
// 안 말해 주면 「+1383」이 「압승 중」으로 읽히고, 후보 목록의 색도 한 줄도 빠짐없이 최대
// 파랑이 된다(evalTone 의 base). 대국·되짚기가 같은 값을 스냅샷과 상세에 실어 보내는
// 것과 같은 자리다(game.Snapshot.BaselineCp · GameDetail.BaselineCp).
//
// 부호를 뒤집지 않는다. 되짚기는 사람이 上手일 수 있어 기준점을 플레이어 관점으로
// 뒤집는데(detailOf), 검토의 관점은 언제나 下手로 못박혀 있다(exploreRoot).
type exploreNode struct {
	whatifNode
	// HandicapJa 는 그 手合割의 이름이다. 平手면 안 온다 — 목록의 HandicapJa 와 같은
	// 규약이고(review.go), 화면이 이름을 만들지 않는다.
	HandicapJa string `json:"handicapJa,omitempty"`
	// BaselineCp 는 그 手合의 「형세 0」이다(先手 관점 cp). 平手면 0이라 안 온다.
	BaselineCp int `json:"baselineCp,omitempty"`
}

func (h *exploreHandler) play(w http.ResponseWriter, r *http.Request) {
	var req exploreRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, exploreBodyLimit)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}
	if len(req.Moves) > exploreMaxLine {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_line", "message": whatifMessages["bad_line"],
		})
		return
	}

	root, hc, ok := exploreRoot(req.Handicap)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_handicap", "message": "その手合割は選べません。",
		})
		return
	}

	// 하나뿐인 벽. 대국이 쓰는 풀에서 하나만 빌린다(exploreSlots).
	slotCtx, cancelSlot := context.WithTimeout(r.Context(), exploreWait)
	defer cancelSlot()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-slotCtx.Done():
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "busy", "message": whatifMessages["busy"],
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), whatifTimeout)
	defer cancel()

	node, err := whatifNodeOf(ctx, root, whatifRequest{Moves: req.Moves}, h.search, cacheOf(h.store))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, exploreNode{
			whatifNode: node,
			HandicapJa: hc.Name,
			BaselineCp: hc.BaselineCp,
		})
	case errors.Is(err, errWhatifMove):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": whatifMessages["bad_move"],
		})
	default:
		// 엔진 고장·시한 초과, 그리고 시작 국면을 못 읽는 경우(errWhatifPly). 뒤엣것은
		// 표가 깨진 것이라 사람이 고칠 일이고, 화면에는 둘 다 「다시 눌러 볼 수 있는
		// 실패」로 나간다 — 검토는 아무것도 안 잃는다.
		log.Printf("explore: handicap %q, %d moves: %v", req.Handicap, len(req.Moves), err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "engine_unavailable", "message": whatifMessages["engine_unavailable"],
		})
	}
}

// exploreRoot 는 手合割 하나를 검토의 뿌리로 옮긴다. 두 번째 값은 그 手合 자체다 —
// 이름과 기준점이 응답에 실린다(exploreNode).
//
// 관점을 下手로 못박는다. 되짚기의 뿌리는 사람이 어느 쪽으로 뒀는가를 들고 있어서
// (whatifRoot.Human) 노드의 cp가 그 사람 관점인데, 검토에는 플레이어가 없다. 先手인 것은
// 手合割이 정한다 — 駒落ち는 上手의 駒를 빼고 그 上手부터 두므로(journal §88) 0手目는
// 「내 차례」가 아니지만, 기준점 표가 下手 관점 cp라(internal/handicap) 관점을 여기로 맞추면
// 화면의 숫자와 「互角ライン」이 같은 자를 쓴다.
//
// Moves 가 비어 있다. 확정된 수가 하나도 없다는 뜻이고, 그래서 요청의 수순이 곧
// 분기 전체다(whatifRequest.Ply 는 0).
func exploreRoot(id string) (whatifRoot, handicap.Handicap, bool) {
	if id == "" {
		// 平手. 빈 StartSFEN 이 平手라는 규약이 이미 있다(game.Config.StartSFEN) —
		// 기준점도 0이라 뺄 것이 없다(internal/handicap).
		return whatifRoot{Human: shogi.Black}, handicap.Handicap{}, true
	}
	h, ok := handicap.Find(id)
	if !ok {
		return whatifRoot{}, handicap.Handicap{}, false
	}
	return whatifRoot{StartSFEN: h.SFEN, Human: shogi.Black}, h, true
}
