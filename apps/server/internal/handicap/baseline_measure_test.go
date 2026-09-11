package handicap_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 표의 기준점을 다시 잰다. Handicap.BaselineCp 가 여기서 나온 값이므로, 엔진이나
// 평가함수를 바꾸면 K와 함께 다시 재야 하는 상수다(01-core.md §2 · journal §84).
//
//	SHOWGI_MEASURE=1 SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/handicap/ -run MeasureBaseline -v
//
// 깊이가 14다. 판정이 쓰는 깊이와 같아야 이 표와 화면 값이 같은 자를 쓴다(journal §130).
//
// 표를 고치지 않는다. 어긋나면 문장으로 말하고 사람이 옮긴다. 자동으로 맞추면 엔진이
// 흔들릴 때마다 판정 기준이 경고 없이 따라 움직인다.
func TestMeasureBaseline(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	// 平手를 같이 잰다. 표에 없는 값이라 여기가 그 숫자를 남기는 하나뿐인 자리다.
	type row struct {
		name string
		sfen string
		want int // 표에 적힌 값. 平手는 0(= 표에 없다)
	}
	rows := []row{{name: "平手", sfen: "", want: 0}}
	for _, h := range handicap.All() {
		rows = append(rows, row{name: h.Name, sfen: h.SFEN, want: h.BaselineCp})
	}

	const depth = 14
	fmt.Printf("\n%-10s %8s %8s %8s %10s %10s\n", "手合", "실측cp", "표cp", "차이", "발화선(옛)", "발화선(지금)")
	for _, r := range rows {
		sfen := r.sfen
		if sfen == "" {
			sfen = hirateSFEN
		}
		res, err := pool.SearchMultiPV(t.Context(), sfen, nil, depth, 1)
		if err != nil {
			t.Errorf("%s: %v", r.name, err)
			continue
		}
		// 駒落ち의 0手目는 上手 차례라 엔진의 관점이 上手다(handicap.Handicap.SFEN). 표는
		// 언제나 下手 관점이므로(Handicap.BaselineCp) 그때 부호를 뒤집는다. 手番을 SFEN
		// 에서 읽어야 이 파일이 그 규약을 두 벌 적지 않는다.
		// 기준점은 언제나 cp 다. 0手目에 詰み이 있을 리 없다.
		got, _ := res.Score.Centipawns()
		if turnOf(sfen) == "w" {
			got = -got
		}
		// 두 칸 다 실측값에서 센다. 「지금」 칸이 기준점을 뺀 자리에서 세므로(got - want)
		// 平手 줄은 두 칸이 같아진다. 기준점 0이 판정을 바꾸지 않는다는 사실이 표에서
		// 그대로 보여야 한다.
		fmt.Printf("%-10s %8d %8d %8d %10d %10d\n",
			r.name, got, r.want, got-r.want, triggerCp(got), triggerCp(got-r.want))
	}
	fmt.Println("\n발화선 = 입문 임계치(0.25)를 넘기는 최소 낙폭. 「옛」이 기준점을 안 쓰던 식이다.")
	fmt.Println("표를 옮길 때는 docs/journal 의 절과 이 패키지 주석의 숫자를 같이 고친다.")
}

// hirateSFEN 은 平手 초기 국면이다. 이 파일이 handicap_test 패키지라 shogi.StartSFEN 을
// 들여오는 대신 값을 직접 갖고 있다.
const hirateSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"

// turnOf 는 SFEN의 手番 칸이다. 읽지 못하면 平手의 手番인 "b" 로 답한다. 그러면 차이
// 칸이 크게 벌어져서 눈에 띈다.
func turnOf(sfen string) string {
	f := strings.Fields(sfen)
	if len(f) < 2 {
		return "b"
	}
	return f[1]
}

// triggerCp 는 from 에서 입문 임계치를 넘기는 최소 낙폭이다. 이 숫자 때문에 이 패키지가
// 있고, 기준점을 쓰지 않은 값과 쓴 값의 차이는 journal §88.
//
// 무엇을 넣느냐가 옛 식과 지금 식을 가른다(위 Printf). 옛 식은 실측 그대로, 지금 식은
// 기준점을 뺀 값이다.
func triggerCp(from int) int {
	const threshold = 0.25
	for d := 1; d <= 20000; d++ {
		if intervene.WinRate(from)-intervene.WinRate(from-d) > threshold {
			return d
		}
	}
	return 0
}
