// 수를 고른다. 규칙은 하나도 모른다.
//
// 서버가 스냅샷마다 legalMoves 를 통째로 주므로 그중 하나를 뽑으면 된다 — 부하 도구에
// 룰 엔진이 필요 없는 이유다. 잘 두려는 것이 아니라 판을 진행시키는 것이 목적이다.

// pickMove 는 합법수 하나를 고른다. 없으면 null.
//
// 成 를 선호한다. 그냥 무작위로 두면 판이 잘 안 끝나서 회차가 手数 상한에만 걸리고,
// 그러면 판정·분석에 들어가는 手数 분포가 실제 대국과 달라진다.
export function pickMove(legalMoves, rng) {
  if (!legalMoves || legalMoves.length === 0) {
    return null;
  }
  const promotions = legalMoves.filter((m) => m.endsWith('+'));
  const pool = promotions.length > 0 && rng() < 0.5 ? promotions : legalMoves;
  return pool[Math.floor(rng() * pool.length)];
}

// makeRNG 는 씨앗이 있는 난수다. Math.random 을 쓰면 회차를 다시 돌릴 수 없다 —
// 같은 씨앗이 같은 수순을 주면 「느렸다」를 같은 판으로 재현할 수 있다.
export function makeRNG(seed) {
  let s = seed >>> 0;
  return function next() {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}
