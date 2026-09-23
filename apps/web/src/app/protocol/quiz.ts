// 문항에는 정답을 싣지 않는다. 채점은 서버가 하고, 정답은 맞힌 뒤의 채점 응답에만 온다.
// 문항에 실으면 화면을 여는 순간 답을 알 수 있다.

export interface QuizPayload {
  /** `false` 는 「아직 만드는 중」이다. 「문항이 없다」가 아니다. */
  ready: boolean;
  /**
   * `ready` 가 거짓일 때, 문항이 아직 만들어질 예정인가. 가져온 판은 평가하는 동안에도 참이다.
   *
   * 없으면 거짓으로 읽지 않는다. 배포 중에는 옛 태스크가 이 필드 없이 답한다.
   */
  queued?: boolean;
  mate?: MateItem;
  best?: BestItem[];
}

export interface MateItem {
  /** 사람은 `ply+1` 手目를 둘 차례다. */
  ply: number;
  sfen: string;
  /** 詰みまでの手数. */
  plies: number;
  /** 사람이 대국에서 이 詰み을 실제로 성공시켰는가. */
  converted: boolean;
  /** 王手인 수만 담는다. */
  legalMoves: string[];
  /** 詰ます 쪽 玉일 수도 있다. 王手를 풀면서 거는 수만 남은 국면이 그렇다. */
  checked?: string;
}

export interface BestItem {
  /** 목록 안의 위치다. 手数가 아니다. */
  index: number;
  ply: number;
  sfen: string;
  /** 王手로 좁히지 않은 합법수 전체다. */
  legalMoves: string[];
  checked?: string;
}

export interface MateAttempt {
  /** 내 수만 담는다. 玉方의 응수는 서버가 정한다. */
  moves: string[];
  /**
   * 1부터 화면이 센다. 서버에 저장하면 되짚기를 다시 열 때마다 「이미 세 번 틀린 문항」으로
   * 시작한다.
   */
  attempt: number;
}

export type MateOutcome = 'ongoing' | 'solved' | 'wrong' | 'not_check';

export interface MateResult {
  /** 내 수와 玉方의 응수가 번갈아 들어 있다. */
  line: string[];
  sfen: string;
  defense?: string;
  defenseJa?: string;
  legalMoves?: string[];
  checked?: string;
  /** 지금 국면의 詰みまでの手数. */
  plies: number;
  outcome: MateOutcome;
  message: string;
  /**
   * 「7九の銀」처럼 움직일 駒만 알려 준다. 세 번째 오답에서만 온다. 오답 응답에 정답 수를 실으면
   * 한 번 틀리는 것으로 문항이 끝난다(회차 2 #10 · #11).
   */
  hint?: string;
}

export interface BestAttempt {
  index: number;
  move: string;
  /** `MateAttempt.attempt` 와 같은 규약이다. */
  attempt: number;
}

export interface BestResult {
  correct: boolean;
  /** 정답과 두 cp 는 맞혔을 때만 온다(회차 2 #10 · #11). */
  answer?: string;
  answerJa?: string;
  /** 사람 관점 cp. */
  answerCp?: number;
  secondCp?: number;
  hint?: string;
  /** 정답에서 이어지는 수순이다. 첫 수가 정답이므로 맞혔을 때만 온다. */
  line?: { usi: string; ja: string; sfen: string }[];
  /**
   * 방금 퀴즈에 낸 수다. `played` 는 그 판에서 실제로 둔 수라 다르다. 둘을 합치면 정답과 打 한
   * 글자만 다른 수를 낸 사람에게 「내가 그 수를 뒀는데 틀렸다고 한다」로 보인다(회차 1 #17).
   */
  move: string;
  moveJa?: string;
  /**
   * 낸 수를 둔 뒤의 국면은 클라이언트가 계산하지 않고 이 값을 쓴다. 없으면 문제 국면을 그대로
   * 둔다(회차 1 #18).
   */
  sfen?: string;
  checked?: string;
  played: string;
  playedJa?: string;
  message: string;
}
