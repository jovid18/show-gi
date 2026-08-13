// 되짚기 퀴즈의 계약. 서버의 `internal/server/quiz.go` 와 짝이다.
//
// **정답이 여기 없다.** 채점은 서버가 하고, 정답은 채점 응답에서 처음 온다 — 문항에 실어
// 보내면 화면을 열자마자 답이 손에 있다.

/** 문항 전부. `GET /api/games/{id}/quiz` */
export interface QuizPayload {
  /**
   * 생성이 끝났는가.
   *
   * **`false` 는 「아직 만드는 중」이고 「문항이 없다」와 다르다.** 만드는 데 수십 초가
   * 걸리므로(판이 끝나는 자리에서 돈다) 그 사이에 「問題はありません」을 그리면 거짓이 된다.
   */
  ready: boolean;
  mate?: MateItem;
  best?: BestItem[];
}

/** 詰み 문항. */
export interface MateItem {
  /** 문제 국면이 만들어진 手数. 사람은 `ply+1` 手目를 두는 차례다. */
  ply: number;
  sfen: string;
  /** 詰みまでの手数. **문항의 일부다** — 몇 手인지 모르면 詰将棋는 풀 수 없다. */
  plies: number;
  /** 사람이 그 詰み을 대국에서 실제로 決めた가. 제목이 여기서 갈린다. */
  converted: boolean;
  /**
   * 둘 수 있는 수 — **王手인 것만**이다.
   *
   * 詰将棋에서 攻方은 매 수 王手를 걸어야 하므로 그 밖은 애초에 문항의 입력이 아니다.
   * 화면은 이 배열만 빛내고, 그래서 「王手가 아닌 수」를 보내는 일이 생기지 않는다.
   */
  legalMoves: string[];
  /**
   * 王手를 받고 있는 玉의 칸.
   *
   * **詰ます 쪽이 자기도 王手를 받고 있을 수 있다** — 王手를 풀면서 거는 수만 남은 국면이다.
   */
  checked?: string;
}

/** 「この局面の最善手は?」 문항. */
export interface BestItem {
  /** 채점 요청이 어느 문항인지 가리키는 값. **手数가 아니다** — 목록의 자리다. */
  index: number;
  ply: number;
  sfen: string;
  /** 합법수 전부. **王手로 좁히지 않는다** — 그 규약은 詰み 문항만의 것이다. */
  legalMoves: string[];
  checked?: string;
}

/** 詰み 문항의 채점 요청. **玉方의 응수는 보내지 않는다** — 서버가 트리에서 꺼내 둔다. */
export interface MateAttempt {
  moves: string[];
  /**
   * 이 문항에서 **몇 번째 시도**인가(1부터). **화면이 센다** — 서버에 남기지 않는다.
   *
   * 몇 번 틀렸는지는 그 판의 사실이 아니라 지금 이 사람이 이 화면에서 하고 있는 일이고,
   * 남기면 되짚기를 다시 열 때마다 「이미 세 번 틀린 문항」이 된다.
   *
   * **이 값으로 정답을 살 수는 없다.** 크게 적어 보내도 오는 것은 `hint` 뿐이다.
   */
  attempt: number;
}

/** 마지막 수가 어떻게 되었나. */
export type MateOutcome = 'ongoing' | 'solved' | 'wrong' | 'not_check';

/** 詰み 문항의 채점 결과이자 다음 장면. */
export interface MateResult {
  /** 판 위에서 진행된 수 전부(내 수와 玉方 응수가 번갈아). */
  line: string[];
  sfen: string;
  /** 직전 내 수에 玉方이 답한 수. 화면이 이것을 짚어야 무엇이 달라졌는지 보인다. */
  defense?: string;
  defenseJa?: string;
  /** 다음에 둘 수 있는 王手들. 끝났으면 없다. */
  legalMoves?: string[];
  checked?: string;
  /** 지금 국면에서 詰みまでの手数. 끝났으면 0. */
  plies: number;
  outcome: MateOutcome;
  /** 화면에 그대로 나가는 일본어. **서버가 만든다.** */
  message: string;
  /**
   * 「무엇을 어디서 움직이나」(「7九の銀」). **세 번째 오답에서만 온다.**
   *
   * **정답 수는 이 응답에 아예 없다.** 첫 오답부터 정답이 실려 오던 자리이고, 그러면 한 번
   * 틀리는 것으로 문항이 끝난다 — 사람이 그걸 지적했다(2026-08-14-human-2.md §6 #10 · #11).
   */
  hint?: string;
}

/** 「최선수는?」 문항의 채점 요청. */
export interface BestAttempt {
  index: number;
  move: string;
  /** 이 문항에서 몇 번째 시도인가(1부터). `MateAttempt.attempt` 와 같은 규약이다. */
  attempt: number;
}

/** 「최선수는?」 문항의 채점 결과. */
export interface BestResult {
  correct: boolean;
  /**
   * 정답과 두 cp. **맞혔을 때만 온다**(2026-08-14-human-2.md §6 #10 · #11).
   *
   * 문구에서 지우는 것으로는 안 됐다 — 응답에 남아 있으면 화면이 그것을 아래 cp 표에
   * 그대로 적고 있었다.
   */
  answer?: string;
  answerJa?: string;
  /** **사람 관점** cp. 둘의 차가 이 문항이 뽑힌 기준이다. */
  answerCp?: number;
  secondCp?: number;
  /** 「무엇을 어디서 움직이나」. **세 번째 오답에서만 온다.** */
  hint?: string;
  /**
   * 정답 뒤에 서로 최선으로 뒀을 때의 흐름. **맞혔을 때만 온다** — 첫 수가 곧 정답이라
   * 이것만 나가도 정답을 말한 것이 된다.
   *
   * **옛 판에는 없다.** 이 칸이 생기기 전에 만들어진 문항은 영영 비어 있고, 화면은
   * 없으면 그 줄을 안 그린다.
   */
  line?: { usi: string; ja: string; sfen: string }[];
  /**
   * 방금 이 문항에 낸 수와 그 棋譜 표기.
   *
   * **`played` 와 다르다** — 저쪽은 그 판에서 실제로 둔 수다. 이 둘을 뭉치고 있었기 때문에
   * 오답 문구가 낸 수를 한 번도 말하지 않았고, 정답과 打 한 글자만 다른 수를 낸 사람에게는
   * 「내가 그것을 뒀는데 틀렸다고 한다」가 됐다(회차 1 #17).
   */
  move: string;
  moveJa?: string;
  /**
   * **그 수를 둔 뒤의 국면.** 없으면 서버가 못 만든 것이고, 그때는 문제 국면을 그대로 둔다.
   *
   * 화면은 규칙을 모르므로 스스로 한 수 둘 수 없다 — 낸 수를 판에서 보여주는 유일한 길이
   * 이 값이다(회차 1 #18).
   */
  sfen?: string;
  /** 그 국면에서 王手를 받고 있는 玉의 칸. 낸 수가 王手였으면 상대 玉이다. */
  checked?: string;
  /** 사람이 대국에서 실제로 둔 수. */
  played: string;
  playedJa?: string;
  message: string;
}
