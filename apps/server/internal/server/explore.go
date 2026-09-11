package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 「検討」 — 手合割을 골라 0手目부터 아무 수나 둬 보면서 형세와 최선수 셋을 읽는 판.
//
// 계산부는 되짚기와 같다(branch.go 의 whatifNodeOf). 갈리는 것은 뿌리 하나뿐이고 그 자리가
// 셋이다. 끝난 판은 DB 기록에서, 두는 중인 판은 세션 스냅샷에서, 여기는 手合割 표에서
// (internal/handicap) 뿌리를 얻는다(journal §85).
//
// 뿌리가 둘이다. 手合割 id와 수순이 하나이고, 판(SFEN) 하나가 다른 하나다. SFEN 을 받는
// 것은 사진에서 읽어 온 국면 때문이고(journal §129), 「아무 국면이나 재 주는 공개 엔진이
// 된다」를 막는 것은 아래 슬롯이다.
//
// SFEN 뿌리에는 지나갈 수순이 없어서 성립하는 판인지를 여기서 본다(exploreRootFault).
//
// 저장·불러오기는 SFEN 을 담지 않는다(explore_snapshots.go). 저장된 값이 곧 다음 요청의
// 본문이라 그쪽에 SFEN 칸을 두면 문이 기록 쪽으로 한 번 더 열린다(journal §96).

const (
	// exploreMaxLine 은 검토 한 줄의 상한이다. 되짚기(whatifMaxLine)보다 길다. 저쪽은 그
	// 手数까지가 이미 기록이고 그 뒤로만 뻗는데, 여기는 0手目부터라 한 판 전체를 걸어 볼 수
	// 있어야 한다.
	exploreMaxLine = 200

	// exploreBodyLimit 은 본문 상한이다. 手合割 id 하나와 수순 한 줄만 온다.
	exploreBodyLimit = 16 << 10

	// exploreSlots 는 이 표면이 동시에 잡을 수 있는 엔진 수다. 이것이 하나뿐인 제한이고,
	// 로그인은 묻지 않는다(journal §100).
	//
	// 풀은 대국이 쓰는 것과 같다(main.go 의 defaultEnginePoolSize). 묶지 않으면 검토 세
	// 건이 엔진을 다 잡고 대국의 착수가 그 뒤에 큐에 서므로, 개입 카드가 늦게 뜬다.
	exploreSlots = 1

	// exploreWait 은 슬롯을 기다리는 시간이다. 그보다 밀리면 「まだ読んでいます」로 답한다.
	//
	// 탐색 하나에 주는 시한과 같은 값이다. 앞사람은 그 시한까지 슬롯을 쥘 수 있으므로,
	// 짧게 잡으면 곧 끝났을 앞사람을 기다리다 포기한다. 꼬리는 길다. 캐시에 없는 국면의
	// p99 가 17.5초다(journal §132).
	exploreWait = whatifTimeout
)

// exploreHandler 는 검토 판의 한 걸음을 답한다. 되짚기와 달리 DB에도 로그인에도 매여 있지
// 않다. 뿌리가 기록 대신 상수 표라 캐시(positions)는 있으면 쓰고, 열리는 기록이 없으니
// 자격을 물을 것도 없다.
type exploreHandler struct {
	// store 는 캐시로만 쓴다. nil이면 답은 같고 같은 국면을 매번 다시 잰다.
	store  *store.Store
	search Searcher
	// slots 는 exploreSlots 만큼 열려 있다. 빈자리가 없으면 exploreWait 만큼만 기다린다.
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
	// Handicap 은 手合割 id다. 빈 값이 平手다(internal/handicap 의 규약).
	Handicap string `json:"handicap"`
	// SFEN 은 뿌리 국면이다. 사진에서 읽어 와 사람이 확인한 판이 여기로 온다.
	//
	// Handicap 과 같이 오지 못한다. 둘 다 뿌리를 정하는 값이라, 같이 오면 어느 쪽이
	// 뿌리인지를 서버가 골라야 하고 그 선택은 화면과 어긋날 수 있다.
	SFEN string `json:"sfen,omitempty"`
	// Moves 는 양쪽 수가 전부 들어 있는 한 줄이다. 서버는 한 수도 대신 두지 않는다.
	Moves []string `json:"moves"`
}

// exploreNode 는 검토의 한 국면이다. 되짚기의 노드에 그 手合의 「형세 0」 두 칸을 얹는다.
//
// 화면이 그 값을 만들 수 없다. 二枚落ち의 0手目가 +1386인데(handicap.BaselineCp) 그것을
// 말해 주지 않으면 「+1383」이 「압승 중」으로 읽히고, 후보 목록의 색도 한 줄도 빠짐없이
// 최대 파랑이 된다(evalTone 의 base).
//
// 부호를 뒤집지 않는다. 되짚기는 사람이 上手일 수 있어 기준점을 플레이어 관점으로
// 뒤집는데(detailOf), 검토의 관점은 언제나 下手로 고정돼 있다(exploreRoot).
type exploreNode struct {
	whatifNode
	// HandicapJa 는 그 手合割의 이름이다. 平手면 오지 않는다(review.go 의 같은 칸과 같은
	// 규약). 화면이 이름을 만들지 않는다.
	HandicapJa string `json:"handicapJa,omitempty"`
	// BaselineCp 는 그 手合의 「형세 0」이다(先手 관점 cp). 平手면 0이라 오지 않는다.
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

	if req.SFEN != "" && req.Handicap != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_root", "message": "手合割と局面は同時に指定できません。",
		})
		return
	}

	if req.SFEN != "" {
		// 지나가면 그 뒤는 手合割 뿌리와 한 줄이다.
		if msg, ok := exploreRootFault(req.SFEN); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "bad_position", "message": msg,
			})
			return
		}
	}

	root, hc, ok := exploreRoot(req.Handicap, req.SFEN)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_handicap", "message": "その手合割は選べません。",
		})
		return
	}

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

	// 이 표면의 탐색은 borrower=explore 다. 풀 대기가 갈려야 「검토가 대국을 기다리게
	// 했나」를 볼 수 있다.
	ctx, cancel := context.WithTimeout(usi.WithBorrower(r.Context(), usi.BorrowerExplore), whatifTimeout)
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
		// 엔진 고장·시한 초과, 그리고 시작 국면을 읽지 못하는 경우(errWhatifPly). 뒤엣것은
		// 표가 깨진 것이라 사람이 고칠 일이고, 화면에는 둘 다 「다시 눌러 볼 수 있는 실패」로
		// 나간다.
		log.Printf("explore: handicap %q, sfen %q, %d moves: %v", req.Handicap, req.SFEN, len(req.Moves), err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "engine_unavailable", "message": whatifMessages["engine_unavailable"],
		})
	}
}

// exploreRoot 는 手合割 하나를 검토의 뿌리로 옮긴다. 두 번째 값은 그 手合 자체이고, 이름과
// 기준점이 응답에 실린다(exploreNode).
//
// 관점을 下手로 고정한다. 되짚기의 뿌리는 사람이 어느 쪽으로 뒀는가를 갖고 있어서
// (whatifRoot.Human) 노드의 cp가 그 사람 관점인데, 검토에는 플레이어가 없다.
//
// 駒落ち는 上手부터 두므로(journal §88) 0手目의 手番은 상대 쪽이다. 그래도 기준점 표가
// 下手 관점 cp라(internal/handicap) 관점을 여기로 맞춰야 화면의 숫자와 「互角ライン」이
// 같은 자를 쓴다.
//
// Moves 가 비어 있다. 확정된 수가 하나도 없다는 뜻이고, 요청의 수순이 곧 분기 전체다
// (whatifRequest.Ply 는 0).
func exploreRoot(id, sfen string) (whatifRoot, handicap.Handicap, bool) {
	if sfen != "" {
		// 手合割이 없다. 임의의 국면에 「형세 0」이 정의되지 않으므로 기준점도 이름도 실리지
		// 않고, 화면이 「互角ライン」을 말하지 않는다.
		//
		// 아래쪽을 先手로 둔 판이다(internal/boardread). 관점을 Black 으로 두는 것이 곧
		// 「사진을 찍은 사람 관점」이다.
		return whatifRoot{StartSFEN: sfen, Human: shogi.Black}, handicap.Handicap{}, true
	}
	if id == "" {
		// 平手. 빈 StartSFEN 이 平手라는 규약이 이미 있고(game.Config.StartSFEN) 기준점도
		// 0이다(internal/handicap).
		return whatifRoot{Human: shogi.Black}, handicap.Handicap{}, true
	}
	h, ok := handicap.Find(id)
	if !ok {
		return whatifRoot{}, handicap.Handicap{}, false
	}
	return whatifRoot{StartSFEN: h.SFEN, Human: shogi.Black}, h, true
}

// exploreRootFault 는 SFEN 뿌리가 성립하는지를 본다. 거짓이면 둘째 값이 화면에 나갈 문구다.
//
// 첫 사유만 말한다. 확인 화면이 이미 사유 전부를 보여 주므로(POST /api/position/check)
// 여기까지 온 것은 그 화면을 지나지 않은 요청이고, 그때 필요한 것은 한 줄이다.
func exploreRootFault(sfen string) (string, bool) {
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return "局面を読み取れませんでした。", false
	}
	if faults := pos.Faults(); len(faults) > 0 {
		return faults[0].Message(), false
	}
	return "", true
}
