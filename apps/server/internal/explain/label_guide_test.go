package explain

import (
	"os"
	"regexp"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 안내 화면(あそびかた)이 카테고리 열 개를 미리 늘어놓는다. 대국 화면·되짚기·총평은
// 서버가 만든 이름을 받아 쓰지만 저기는 실제로 걸린 개입이 없어 받을 데가 없고, 그래서
// 이 레포에서 어휘가 두 벌인 유일한 자리다.
//
// 그 한 벌을 여기서 잠근다 — 카테고리를 늘리거나 이름을 고치면 이 테스트가 깨지고,
// 안 고치면 안내 화면만 옛 이름으로 남는다. 코드 쪽 사정은 categories.ts 에 적어 뒀다.
const guideCategoriesPath = "../../../web/src/app/libs/game/categories.ts"

// { code: 'missed_mate', nameJa: '詰み逃し', note: … } 에서 앞의 둘만 뗀다.
// oxfmt가 작은따옴표로 맞추므로 그 형태만 본다 — 형식이 바뀌면 0개가 잡혀서 아래가 잡는다.
var guideEntry = regexp.MustCompile(`code: '([a-z_]+)', nameJa: '([^']+)'`)

func TestGuideCategoriesMatchServer(t *testing.T) {
	src, err := os.ReadFile(guideCategoriesPath)
	if err != nil {
		t.Fatalf("안내 화면의 카테고리 목록을 못 읽었다: %v", err)
	}

	got := map[string]string{}
	for _, m := range guideEntry.FindAllStringSubmatch(string(src), -1) {
		if _, dup := got[m[1]]; dup {
			t.Errorf("%s 가 두 번 적혀 있다", m[1])
		}
		got[m[1]] = m[2]
	}

	// other 까지 센다. categoryNames 에는 있지만 미분류라 잊기 쉽고,
	// 빠지면 안내 화면이 「열 종류」라고 써 놓고 아홉만 보여준다.
	want := map[string]string{}
	for c := range categoryNames {
		want[string(c)] = CategoryJa(c)
	}

	if len(got) != len(want) {
		t.Fatalf("개수가 다르다: 안내 %d개, 서버 %d개 (%s)", len(got), len(want), guideCategoriesPath)
	}

	for code, name := range want {
		switch g, ok := got[code]; {
		case !ok:
			t.Errorf("안내 화면에 %s 가 없다 (서버는 「%s」)", code, name)
		case g != name:
			t.Errorf("%s 의 이름이 다르다: 안내 「%s」, 서버 「%s」", code, g, name)
		}
	}

	// 서버가 모르는 코드를 안내가 갖고 있으면 지운 카테고리가 화면에 남은 것이다.
	for code := range got {
		if _, ok := want[code]; !ok {
			t.Errorf("서버에 없는 카테고리가 안내 화면에 있다: %s", code)
		}
	}

	// 이름을 안 지어냈는지 한 번 더 본다 — 위 표가 CategoryJa 를 거쳐 왔으므로
	// 여기까지 왔으면 미분류의 이름도 서버 것이다.
	if got["other"] != CategoryJa(intervene.CategoryOther) {
		t.Errorf("미분류의 이름이 서버와 다르다")
	}
}
