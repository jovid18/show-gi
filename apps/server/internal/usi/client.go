// Package usi 는 USI(Universal Shogi Interface) 엔진 하위 프로세스를 관리한다.
//
// 값어치는 기능이 아니라 **방어**다 — 전부 한 번씩 물려본 것들이고 목록은 02-architecture.md §8에 있다.
// Engine 하나는 탐색을 직렬화한다. 동시 탐색이 필요하면 Pool을 쓴다.
package usi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	handshakeTimeout = 15 * time.Second

	// stopGrace 는 취소로 "stop"을 보낸 뒤 bestmove를 기다려주는 시간. 안 오면 엔진을 버리고 재기동한다 —
	// 삼키지 못한 bestmove는 **다음 탐색의 결과로 읽힌다**(06-status.md §6 ②).
	stopGrace = 2 * time.Second
)

// MateCp 는 mate 점수를 cp로 환산할 때의 상한이다.
const MateCp = 30000

// SearchLine 은 info 라인 하나 (점수는 수번 측 관점).
type SearchLine struct {
	Depth   int
	MultiPV int // 1부터. MultiPV 미사용이면 항상 1
	Move    string
	ScoreCp int
	IsMate  bool
	MateIn  int
	PV      []string
}

// SearchResult 는 go 커맨드 한 번의 결과.
type SearchResult struct {
	Best    string // USI 수, 또는 "resign"/"win"/"none"
	Depth   int    // 도달한 최대 깊이
	ScoreCp int    // 수번 측 관점 centipawn (mate는 환산값)
	IsMate  bool
	MateIn  int          // IsMate일 때: 양수 = 수번 측이 이김
	PV      []string     // 최선 수순
	Lines   []SearchLine // 순위별 최종 후보 (가장 깊은 것)

	// History 는 받은 info 라인 전부를 (깊이, 순위)별로 남긴 것이다.
	// **얕은 평가와 깊은 평가의 격차**가 개입 판정의 입력이라 마지막 깊이만 남기면 안 된다(06-status.md §6 ②).
	// 속보(lowerbound/upperbound)는 점수가 확정값이 아니라 넣지 않는다.
	History []SearchLine
}

// Ranked 는 후보 줄을 **정본 순서**로 준다 — 점수 내림차순이고, 빈 순위와 중복 순위는 빠진다.
//
// **이 순서가 한 자리에만 있어야 한다.** 캐시에 쌓이는 후보 목록(archive.Candidates)과
// 개입 문장이 말하는 상대의 최선수(game.engineAnalyst.cardPV)가 둘 다 이것을 보고, 갈리면
// 한 국면의 최선수가 화면에서 둘이 된다(06-status.md §58).
//
// **`Lines[0]` 이 1위가 아닐 수 있다.** 순위별 자리를 미리 채워 두므로(`parseScore`) 아직 안
// 온 순위는 빈 줄로 남고, 그것을 그대로 1위로 읽으면 수가 없는 후보를 최선수라고 부른다.
func (r SearchResult) Ranked() []SearchLine {
	out := make([]SearchLine, 0, len(r.Lines))
	seen := map[int]bool{}
	for _, l := range r.Lines {
		if l.Move == "" || seen[l.MultiPV] {
			continue
		}
		seen[l.MultiPV] = true
		out = append(out, l)
	}
	slices.SortStableFunc(out, func(x, y SearchLine) int { return y.ScoreCp - x.ScoreCp })
	return out
}

// DepthEval 은 한 수의 특정 깊이 평가치다.
type DepthEval struct {
	Depth int
	Cp    int
}

// EvalByDepth 는 그 수의 깊이별 평가치를 오름차순으로 준다. edges.eval_by_depth 에 그대로 들어간다.
// 빠진 깊이를 메우지 않는다 — 상위 k에 없던 깊이는 데이터가 없는 것이다(06-status.md §6 ②).
func (r SearchResult) EvalByDepth(move string) []DepthEval {
	var out []DepthEval
	for _, l := range r.History {
		if l.Move == move {
			out = append(out, DepthEval{Depth: l.Depth, Cp: l.ScoreCp})
		}
	}
	slices.SortFunc(out, func(a, b DepthEval) int { return a.Depth - b.Depth })
	return out
}

// ScoreAtDepth 는 그 깊이에서 1위였던 줄의 점수다 — 「이 국면을 여기까지만 읽으면 얼마로 보이나」.
// 초보자의 시야를 모사하는 쪽이 이것이다(06-status.md §15). 없는 깊이를 메우지 않는다.
func (r SearchResult) ScoreAtDepth(depth int) (int, bool) {
	for _, l := range r.History {
		if l.Depth == depth && l.MultiPV == 1 {
			return l.ScoreCp, true
		}
	}
	return 0, false
}

// Engine 은 USI 엔진 1개. 프로세스가 죽으면 다음 호출에서 재기동한다.
// 모든 공개 메서드는 mu로 직렬화된다 — 즉 프로세스 1개 = 동시 탐색 1개.
type Engine struct {
	mu   sync.Mutex
	path string
	args []string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	name   string
	opts   map[string]bool
	saved  map[string]string // 재기동 시 복원할 setoption
	closed bool
}

// New 는 엔진 프로세스를 시작하고 usi/isready 핸드셰이크를 마친다.
// opts 는 **usiok 뒤, isready 앞**에 걸린다 — USI_Hash 처럼 isready 시점에 반영되는 옵션은 나중에 걸면 늦는다.
func New(path string, opts map[string]string, args ...string) (*Engine, error) {
	saved := make(map[string]string, len(opts))
	maps.Copy(saved, opts)

	e := &Engine{path: path, args: args, saved: saved}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.start(); err != nil {
		return nil, err
	}
	return e, nil
}

// start 는 mu를 잡은 상태에서 호출해야 한다.
func (e *Engine) start() error {
	cmd := exec.Command(e.path, e.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start engine %s: %w", e.path, err)
	}

	lines := make(chan string, 512)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		// 읽기가 에러로 끝났으면 남긴다. 채널이 닫히는 것은 프로세스가 죽었을 때와
		// 같아서, 안 남기면 "엔진이 죽었다"로만 보이고 원인(예: 한 줄이 너무 길다)이 묻힌다.
		if err := sc.Err(); err != nil {
			log.Printf("usi: reading engine output failed (%s): %v", e.path, err)
		}
		close(lines)
		_ = cmd.Wait()
	}()

	e.cmd = cmd
	e.stdin = stdin
	e.lines = lines
	e.opts = map[string]bool{}

	if err := e.handshake(); err != nil {
		e.kill()
		return err
	}
	return nil
}

func (e *Engine) handshake() error {
	if err := e.send("usi"); err != nil {
		return err
	}
	deadline := time.After(handshakeTimeout)
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				return errors.New("engine exited before usiok")
			}
			if name, ok := strings.CutPrefix(line, "id name "); ok {
				e.name = name
			}
			if rest, ok := strings.CutPrefix(line, "option name "); ok {
				if i := strings.Index(rest, " type "); i > 0 {
					e.opts[rest[:i]] = true
				}
			}
			if line == "usiok" {
				goto ready
			}
		case <-deadline:
			return fmt.Errorf("usiok timeout (%s)", e.path)
		}
	}
ready:
	// 변형이 shogi가 아닐 가능성을 막는다.
	// 이름이 엔진마다 다르다 — fairy-stockfish는 UCI_Variant만 광고한다.
	for _, opt := range []string{"USI_Variant", "UCI_Variant"} {
		if e.opts[opt] {
			if err := e.send("setoption name " + opt + " value shogi"); err != nil {
				return err
			}
			break
		}
	}
	if e.opts["USI_Ponder"] {
		_ = e.send("setoption name USI_Ponder value false")
	}

	// PvInterval=0. **배포 설정이 아니라 이 파서가 동작하기 위한 조건이다** —
	// 기본 간격이면 우리 탐색이 더 빨라 깊이별 평가치가 마지막 하나만 남는다(06-status.md §10).
	if e.opts["PvInterval"] {
		_ = e.send("setoption name PvInterval value 0")
	}

	// 저장된 옵션은 **isready 앞**에서 건다. 재기동 때 복원되는 경로도 여기다 —
	// USI_Hash 처럼 isready 에서 반영되는 옵션이 재기동 후에 빠지면, 살아난 엔진만
	// 조용히 다른 설정으로 돌게 된다.
	for name, val := range e.saved {
		if !e.opts[name] {
			continue // 엔진이 모르는 옵션은 보내지 않는다
		}
		if err := e.setOptionLocked(name, val); err != nil {
			return err
		}
	}
	return e.syncReady()
}

func (e *Engine) syncReady() error {
	if err := e.send("isready"); err != nil {
		return err
	}
	deadline := time.After(handshakeTimeout)
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				return errors.New("engine exited before readyok")
			}
			if line == "readyok" {
				return nil
			}
		case <-deadline:
			return errors.New("readyok timeout")
		}
	}
}

func (e *Engine) send(s string) error {
	_, err := io.WriteString(e.stdin, s+"\n")
	return err
}

func (e *Engine) kill() {
	if e.stdin != nil {
		_ = e.stdin.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
}

// restart 는 mu를 잡은 상태에서 호출.
func (e *Engine) restart() error {
	log.Printf("usi: restarting engine (%s)", e.path)
	e.kill()
	for range e.lines { // 남은 라인 드레인
	}
	return e.start()
}

func (e *Engine) setOptionLocked(name, value string) error {
	return e.send(fmt.Sprintf("setoption name %s value %s", name, value))
}

// SetOption 은 옵션을 설정하고 재기동 시에도 복원되도록 기억한다.
func (e *Engine) SetOption(name, value string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.saved[name] = value
	if !e.opts[name] {
		return nil // 엔진이 모르는 옵션은 조용히 무시
	}
	return e.setOptionLocked(name, value)
}

// SetMultiPV 는 후보 수를 몇 개까지 받을지 정한다.
func (e *Engine) SetMultiPV(n int) error {
	return e.SetOption("MultiPV", strconv.Itoa(n))
}

// HasOption 은 엔진이 그 옵션을 광고했는지 돌려준다.
func (e *Engine) HasOption(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.opts[name]
}

func (e *Engine) Name() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.name == "" {
		return e.path
	}
	return e.name
}

// SearchDepth 는 고정 깊이까지 탐색시킨다. **시간 기반(`go movetime`)은 이 패키지에 일부러 없다** —
// 재현되지 않으면 캐시도 밴드 제어도 성립하지 않는다(01-core.md §4). 자체 시한도 없다.
// ctx로 끊을 수는 있고, 끊으면 중간 결과는 **버린다** — depth N 결과가 아니라서 쓸 수 없다.
func (e *Engine) SearchDepth(ctx context.Context, startSFEN string, moves []string, depth int) (SearchResult, error) {
	return e.search(ctx, startSFEN, moves, "go depth "+strconv.Itoa(depth), 0)
}

// MateResult 는 詰み 탐색 한 번의 결과다.
type MateResult struct {
	// Moves 는 찾은 詰み 수순. 비어 있으면 못 찾았다.
	Moves []string

	// Proven 은 탐색이 한계 안에서 **결론을 냈다**는 뜻이고, 이 구분이 캐시의 전부다.
	// `checkmate timeout`은 "모른다"이지 "없다"가 아니다 — "없다"로 저장하면 있는 詰み을 놓친다(01-core.md §2).
	Proven bool
}

// Found 는 詰み을 찾았는지.
func (r MateResult) Found() bool { return len(r.Moves) > 0 }

// SearchMate 는 詰み을 찾는다. **詰将棋 solver 에디션에만 있다** — 탐색부는 bestmove로 답한다(02-architecture.md §3).
// 한계는 풀을 만들 때 `DepthLimit`(手数)로 준다. 없이 부르면 詰み이 없는 국면에서 돌아오지 않는다.
func (e *Engine) SearchMate(ctx context.Context, startSFEN string, moves []string) (MateResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	res, err := e.searchMateLocked(ctx, startSFEN, moves)
	if err == nil {
		return res, nil
	}
	if ctx.Err() != nil {
		return MateResult{}, err
	}
	if rerr := e.restart(); rerr != nil {
		return MateResult{}, fmt.Errorf("mate search failed (%v) and restart failed: %w", err, rerr)
	}
	return e.searchMateLocked(ctx, startSFEN, moves)
}

func (e *Engine) searchMateLocked(ctx context.Context, startSFEN string, moves []string) (MateResult, error) {
	pos := "position sfen " + startSFEN
	if len(moves) > 0 {
		pos += " moves " + strings.Join(moves, " ")
	}
	if err := e.send(pos); err != nil {
		return MateResult{}, err
	}
	if err := e.send("go mate infinite"); err != nil {
		return MateResult{}, err
	}

	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				return MateResult{}, errors.New("engine exited during mate search")
			}
			rest, isMate := strings.CutPrefix(line, "checkmate")
			if !isMate {
				continue
			}
			switch fields := strings.Fields(rest); {
			case len(fields) == 0:
				return MateResult{}, errors.New("empty checkmate response")
			case fields[0] == "nomate":
				return MateResult{Proven: true}, nil
			case fields[0] == "timeout":
				return MateResult{}, nil // 모른다. Proven=false
			case fields[0] == "notimplemented":
				return MateResult{}, errors.New("engine does not implement go mate")
			default:
				return MateResult{Moves: filterMoves(fields), Proven: true}, nil
			}

		case <-ctx.Done():
			if err := e.stopLocked(); err != nil {
				log.Printf("usi: engine did not answer stop during mate search (%v), restarting", err)
				if rerr := e.restart(); rerr != nil {
					log.Printf("usi: restart after stop failed: %v", rerr)
				}
			}
			return MateResult{}, ctx.Err()
		}
	}
}

func (e *Engine) search(ctx context.Context, startSFEN string, moves []string, goCmd string, timeout time.Duration) (SearchResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	res, err := e.searchLocked(ctx, startSFEN, moves, goCmd, timeout)
	if err == nil {
		return res, nil
	}
	// 취소는 고장이 아니다. 재시도하면 부른 쪽이 그만두라고 한 일을 한 번 더 하게 된다.
	if ctx.Err() != nil {
		return SearchResult{}, err
	}
	if rerr := e.restart(); rerr != nil {
		return SearchResult{}, fmt.Errorf("search failed (%v) and restart failed: %w", err, rerr)
	}
	return e.searchLocked(ctx, startSFEN, moves, goCmd, timeout)
}

func (e *Engine) searchLocked(ctx context.Context, startSFEN string, moves []string, goCmd string, timeout time.Duration) (SearchResult, error) {
	pos := "position sfen " + startSFEN
	if len(moves) > 0 {
		pos += " moves " + strings.Join(moves, " ")
	}
	if err := e.send(pos); err != nil {
		return SearchResult{}, err
	}
	if err := e.send(goCmd); err != nil {
		return SearchResult{}, err
	}

	var res SearchResult
	// timeout 0 = 무제한: nil 채널 수신은 영원히 블록되므로 deadline 분기가 사라진다
	var deadline <-chan time.Time
	if timeout > 0 {
		deadline = time.After(timeout)
	}
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				return SearchResult{}, errors.New("engine exited during search")
			}
			if strings.HasPrefix(line, "info ") {
				parseScore(line, &res)
			}
			if strings.HasPrefix(line, "bestmove") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					res.Best = fields[1]
				} else {
					res.Best = "none"
				}
				return res, nil
			}

		case <-deadline:
			return SearchResult{}, errors.New("bestmove timeout")

		case <-ctx.Done():
			// stop을 보내고 bestmove를 반드시 삼킨다. 안 삼키면 다음 탐색이 그걸 읽는다.
			if err := e.stopLocked(); err != nil {
				log.Printf("usi: engine did not answer stop (%v), restarting", err)
				if rerr := e.restart(); rerr != nil {
					log.Printf("usi: restart after stop failed: %v", rerr)
				}
			}
			return SearchResult{}, ctx.Err()
		}
	}
}

// stopLocked 는 탐색을 중단시키고 그 응답까지 읽어 버린다. mu를 잡은 상태에서 호출.
// **끝맺는 말이 다르다** — 일반 탐색은 `bestmove`, 詰み 탐색은 `checkmate`. 하나만 기다리면 매번 엔진을 버린다.
func (e *Engine) stopLocked() error {
	if err := e.send("stop"); err != nil {
		return err
	}
	deadline := time.After(stopGrace)
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				return errors.New("engine exited before it answered stop")
			}
			if strings.HasPrefix(line, "bestmove") || strings.HasPrefix(line, "checkmate") {
				return nil
			}
		case <-deadline:
			return errors.New("no bestmove/checkmate after stop")
		}
	}
}

func parseScore(line string, res *SearchResult) {
	fields := strings.Fields(line)
	var sl SearchLine
	sl.MultiPV = 1 // multipv 표기가 없으면 1위
	hasScore := false
	bound := false // fail-high/low 속보 라인 (score가 확정값이 아님)
	for i := 0; i+1 < len(fields); i++ {
		switch fields[i] {
		case "lowerbound", "upperbound":
			bound = true
		case "depth":
			if v, err := strconv.Atoi(fields[i+1]); err == nil && v >= 0 {
				sl.Depth = v
			}
		case "multipv":
			if v, err := strconv.Atoi(fields[i+1]); err == nil && v >= 1 {
				sl.MultiPV = v
			}
		case "score":
			kind := fields[i+1]
			if i+2 >= len(fields) {
				return
			}
			v, err := strconv.Atoi(fields[i+2])
			if err != nil {
				return
			}
			hasScore = true
			switch kind {
			case "cp":
				sl.ScoreCp = v
			case "mate":
				sl.IsMate = true
				sl.MateIn = v
				if v >= 0 {
					sl.ScoreCp = MateCp - 10*v
				} else {
					sl.ScoreCp = -MateCp - 10*v
				}
			}
		case "pv":
			// pv는 항상 info 라인의 마지막 필드들
			sl.PV = filterMoves(fields[i+1:])
			if len(sl.PV) > 0 {
				sl.Move = sl.PV[0]
			}
			goto apply
		}
	}
apply:
	if !hasScore && len(sl.PV) == 0 {
		return
	}
	if sl.Depth > res.Depth {
		res.Depth = sl.Depth
	}

	// 깊이별 기록. 속보는 점수가 확정값이 아니라 넣지 않는다.
	if hasScore && !bound && len(sl.PV) > 0 {
		recordHistory(res, sl)
	}

	idx := sl.MultiPV
	// 짧은 pv가 이미 받아둔 긴 수순을 덮어쓰지 않게 방어한다 — 속보(bound) 라인과,
	// 엔진이 마지막 iteration을 중간에 접었을 때가 그렇다. 빈 순위라면 짧은 라인이라도 채워 둔다.
	const minUsefulPv = 3
	if idx-1 < len(res.Lines) {
		prev := res.Lines[idx-1].PV
		if (bound || len(sl.PV) < minUsefulPv) && len(prev) > len(sl.PV) {
			return
		}
	}
	// 1위 라인은 기존 필드에도 반영 (MultiPV 미사용 호출과 호환)
	if idx == 1 {
		if hasScore {
			res.ScoreCp = sl.ScoreCp
			res.IsMate = sl.IsMate
			res.MateIn = sl.MateIn
		}
		if len(sl.PV) > 0 {
			res.PV = sl.PV
		}
	}
	if hasScore && len(sl.PV) > 0 {
		for len(res.Lines) < idx {
			res.Lines = append(res.Lines, SearchLine{})
		}
		res.Lines[idx-1] = sl
	}
}

// recordHistory 는 (깊이, 순위)당 한 줄만 남긴다. 같은 자리가 다시 오면 나중 것이 이긴다.
func recordHistory(res *SearchResult, sl SearchLine) {
	for i := range res.History {
		if res.History[i].Depth == sl.Depth && res.History[i].MultiPV == sl.MultiPV {
			res.History[i] = sl
			return
		}
	}
	res.History = append(res.History, sl)
}

var usiMoveRe = regexp.MustCompile(`^(?:[1-9][a-i][1-9][a-i]\+?|[PLNSGBR]\*[1-9][a-i])$`)

// filterMoves 는 pv 필드에서 USI 수 형식만 남긴다 (엔진별 잡토큰 방어).
func filterMoves(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !usiMoveRe.MatchString(f) {
			break
		}
		out = append(out, f)
	}
	return out
}

// Close 는 엔진을 종료한다.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	_ = e.send("quit")
	time.Sleep(50 * time.Millisecond)
	e.kill()
}
