# lila AccuracyPercent.gameAccuracy 를 줄 단위로 옮긴 참조. 입력은 승률(0~100, 先手=white 관점).
import math, json, random
def from_wp(before, after):
    if after >= before: return 100.0
    raw = 103.1668100711649 * math.exp(-0.04354415386753951 * (before - after)) + -3.166924740191411
    return min(max(raw + 1, 0), 100)
def sd(xs):
    m = sum(xs)/len(xs); return math.sqrt(sum((x-m)**2 for x in xs)/len(xs))
def sliding(xs, n):
    if len(xs) <= n: return [xs]
    return [xs[i:i+n] for i in range(len(xs)-n+1)]
def game(start_white, all_wp):  # all_wp includes initial
    n_cps = len(all_wp) - 1
    ws = min(max(n_cps // 10, 2), 8)
    windows = [all_wp[:ws]] * max(min(ws, len(all_wp)) - 2, 0) + sliding(all_wp, ws)
    weights = [None if any(v is None for v in w) else min(max(sd(w), 0.5), 12) for w in windows]
    out = {}
    pairs = [(all_wp[i], all_wp[i+1]) for i in range(len(all_wp)-1)]
    acc = []
    for i, ((p, nx), w) in enumerate(zip(pairs, weights)):
        white = (i % 2 == 0) == start_white
        if p is None or nx is None or w is None: acc.append((None, white)); continue
        a = from_wp(p, nx) if white else from_wp(nx, p)
        acc.append(((a, w), white))
    for color in (True, False):
        xs = [x for x, c in acc if c == color and x is not None]
        if not xs: out[color] = None; continue
        wm = sum(a*w for a, w in xs) / sum(w for _, w in xs)
        hm = len(xs) / sum(1/max(1, a) for a, _ in xs)
        out[color] = (wm + hm) / 2
    return out
random.seed(7)
cases = []
for n, holes in [(1, 0), (7, 0), (40, 0), (120, 3), (25, 1)]:
    wp = [50.0]; x = 50.0
    for i in range(n):
        x = min(max(x + random.gauss(0, 8), 0), 100); wp.append(round(x, 3))
    for _ in range(holes): wp[random.randrange(1, n+1)] = None
    for sw in (True, False):
        r = game(sw, wp)
        cases.append({"firstBlack": sw, "wins": wp, "black": r[True], "white": r[False]})
print(json.dumps(cases))
