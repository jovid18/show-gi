package metrics

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

// WriteText 는 Prometheus 텍스트 형식으로 쓴다.
//
// 이 표면은 태스크 안에서만 닿는다 — Caddy 가 /ws·/api·/healthz 만 프록시하므로
// 새 경로는 밖에서 보이지 않는다(apps/web/Caddyfile). 그래서 인증을 따로 두지 않았다.
//
// 다 만든 뒤에 한 번 쓴다. w 에 직접 쓰면서 잠금을 잡고 있으면, 읽는 쪽이 중간에
// 멈춘 순간(ECS Exec 세션이 끊긴다) 그 계열의 잠금이 안 풀리고 — 서버에 WriteTimeout
// 이 없다 — 모든 요청과 엔진 대여가 같이 멈춘다.
func (r *Registry) WriteText(w io.Writer) error {
	bw := &bytes.Buffer{}

	r.mu.Lock()
	families := append([]*family(nil), r.families...)
	r.mu.Unlock()

	for _, f := range families {
		bw.WriteString("# HELP ")
		bw.WriteString(f.name)
		bw.WriteByte(' ')
		bw.WriteString(strings.ReplaceAll(f.help, "\n", " "))
		bw.WriteString("\n# TYPE ")
		bw.WriteString(f.name)
		bw.WriteByte(' ')
		bw.WriteString(string(f.kind))
		bw.WriteByte('\n')

		f.mu.Lock()
		for _, s := range f.order {
			if f.kind == kindHistogram {
				writeHistogram(bw, f, s)
				continue
			}
			writeLine(bw, f.name, "", labelText(f.labels, s.labelValues, "", ""), num(s.value))
		}
		f.mu.Unlock()
	}

	_, err := w.Write(bw.Bytes())
	return err
}

// writeHistogram 은 계열 하나를 버킷·합·개수 세 종류로 쓴다.
//
// 버킷은 누적이다. counts 가 이미 「경계 이하의 관측 수」로 쌓여 있고(Observe),
// 마지막 +Inf 는 전체 관측 수와 같다.
func writeHistogram(bw *bytes.Buffer, f *family, s *series) {
	for i, b := range f.buckets {
		writeLine(bw, f.name, "_bucket", labelText(f.labels, s.labelValues, "le", num(b)), strconv.FormatUint(s.counts[i], 10))
	}
	writeLine(bw, f.name, "_bucket", labelText(f.labels, s.labelValues, "le", "+Inf"), strconv.FormatUint(s.count, 10))
	writeLine(bw, f.name, "_sum", labelText(f.labels, s.labelValues, "", ""), num(s.sum))
	writeLine(bw, f.name, "_count", labelText(f.labels, s.labelValues, "", ""), strconv.FormatUint(s.count, 10))
}

func writeLine(bw *bytes.Buffer, name, suffix, labels, value string) {
	bw.WriteString(name)
	bw.WriteString(suffix)
	bw.WriteString(labels)
	bw.WriteByte(' ')
	bw.WriteString(value)
	bw.WriteByte('\n')
}

// labelText 는 라벨 부분을 만든다. extraName 이 비어 있지 않으면 뒤에 하나 더 붙인다(le).
func labelText(names, values []string, extraName, extraValue string) string {
	if len(names) == 0 && extraName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteString(`="`)
		b.WriteString(escape(values[i]))
		b.WriteByte('"')
	}
	if extraName != "" {
		if len(names) > 0 {
			b.WriteByte(',')
		}
		b.WriteString(extraName)
		b.WriteString(`="`)
		b.WriteString(escape(extraValue))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// escape 는 라벨 값에서 형식을 깨뜨리는 세 글자를 막는다.
func escape(v string) string {
	if !strings.ContainsAny(v, `\"`+"\n") {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return strings.ReplaceAll(v, "\n", `\n`)
}

// num 은 실수를 가장 짧은 표기로 쓴다. 지수 표기가 나와도 Prometheus 는 그대로 읽는다.
func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
