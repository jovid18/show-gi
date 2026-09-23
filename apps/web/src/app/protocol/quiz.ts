// 되짚기 퀴즈의 계약. 서버의 `internal/server/quiz.go` 와 짝이다.
//
// 정답은 여기에 없다. 채점은 서버가 하고, 정답은 채점 응답에 처음 실려 온다. 문항에 정답을
// 실으면 화면을 열자마자 답을 알 수 있다.

/** 문항 전체. `GET /api/games/{id}/quiz` */
export interface QuizPayload {
  /**
   * 문항 생성이 끝났는가. `false` 는 「아직 만드는 중」이고, 「문항이 없다」와
   * 다르다(`useQuiz`).
   */
  ready: boolean;
  /**
   * 문항이 아직 만들어지는 중인가. `ready` 가 거짓일 때만 의미가 있다.
   *
   * 둘 다 거짓이면 화면은 더 기다리지 않는다.
   *
   * 큐만 보고 정하지 않는다. 가져온 판은 모든 수의 평가를 마친 뒤에야 문항 생성을 큐에
   * 넣는다. 그래서 평가하는 동안에도 참이다. 가져온 직후에 퀴즈를 열면 이 경우에 해당한다.
   *
   * 없을 수 있다. 배포 중에는 옛 태스크가 이 필드 없이 응답한다. 이때 필드가 없다는 것은
   * 「만드는 중이 아니다」가 아니라 「알려 주지 않았다」다.
   */
  queued?: boolean;
  mate?: MateItem;
  best?: BestItem[];
}

/** 詰み 문항. */
export interface MateItem {
  /** 문제 국면의 手数. 사람은 `ply+1` 手目를 둘 차례다. */
  ply: number;
  sfen: string;
  /** 詰みまでの手数. 몇 手인지 모르면 詰将棋를 풀 수 없으므로 문항에 포함한다. */
  plies: number;
  /** 사람이 대국에서 이 詰み을 실제로 성공시킨 판인가. 이 값에 따라 제목이 달라진다. */
  converted: boolean;
  /**
   * 둘 수 있는 수. 王手만 들어 있다.
   *
   * 詰将棋에서 攻方은 매 수 王手를 걸어야 하므로 나머지 수는 입력에서 뺀다. 화면은 이 배열의
   * 수만 강조하므로 王手가 아닌 수를 보낼 일이 없다.
   */
  legalMoves: string[];
  /**
   * 王手를 받고 있는 玉의 칸.
   *
   * 詰ます 쪽 玉이 王手를 받고 있을 수도 있다. 王手를 피하면서 동시에 王手를 거는 수만 남은
   * 국면이 그렇다.
   */
  checked?: string;
}

/** 「この局面の最善手は?」 문항. */
export interface BestItem {
  /** 채점 요청이 어느 문항인지 가리키는 값. 手数가 아니라 목록 안의 위치다. */
  index: number;
  ply: number;
  sfen: string;
  /** 합법수 전체. 王手만 남기는 규약은 詰み 문항에만 적용한다. */
  legalMoves: string[];
  checked?: string;
}

/** 詰み 문항의 채점 요청. 玉方의 응수는 보내지 않는다. 서버가 트리에서 골라 둔다. */
export interface MateAttempt {
  moves: string[];
  /**
   * 이 문항에서 몇 번째 시도인가(1부터). 화면이 세고, 서버는 저장하지 않는다. 저장하면
   * 되짚기를 다시 열 때마다 「이미 세 번 틀린 문항」으로 시작한다.
   *
   * 이 값으로 정답을 받아 낼 수는 없다. 값을 크게 부풀려 보내도 돌아오는 것은 `hint` 뿐이다.
   */
  attempt: number;
}

/** 마지막 수의 결과. */
export type MateOutcome = 'ongoing' | 'solved' | 'wrong' | 'not_check';

/** 詰み 문항의 채점 결과이자 다음 장면. */
export interface MateResult {
  /** 판 위에서 진행된 수 전체. 내 수와 玉方의 응수가 번갈아 들어 있다. */
  line: string[];
  sfen: string;
  /** 직전 내 수에 玉方이 응수한 수. 화면이 이 수를 보여 줘야 무엇이 달라졌는지 알 수 있다. */
  defense?: string;
  defenseJa?: string;
  /** 다음에 둘 수 있는 王手들. 문항이 끝났으면 없다. */
  legalMoves?: string[];
  checked?: string;
  /** 현재 국면에서 詰みまでの手数. 문항이 끝났으면 0. */
  plies: number;
  outcome: MateOutcome;
  /** 화면에 그대로 표시하는 일본어. 서버가 만든다. */
  message: string;
  /**
   * 「무엇을 어디서 움직이나」(「7九の銀」). 세 번째 오답에서만 온다.
   *
   * 정답 수는 이 응답에 아예 없다. 첫 오답에 정답을 실어 보내면 한 번만 틀려도 답을 알게 되어
   * 문항이 끝난다(회차 2 #10 · #11).
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
  /** 정답 수와 두 cp. 맞혔을 때만 온다(회차 2 #10 · #11). */
  answer?: string;
  answerJa?: string;
  /** 사람 관점 cp. 두 값의 차이가 이 문항을 고른 기준이다. */
  answerCp?: number;
  secondCp?: number;
  /** 「무엇을 어디서 움직이나」. 세 번째 오답에서만 온다. */
  hint?: string;
  /**
   * 정답 뒤에 양쪽이 최선으로 뒀을 때의 수순. 맞혔을 때만 온다. 첫 수가 곧 정답이므로 이 값만
   * 보내도 정답이 드러난다.
   *
   * 이 필드가 생기기 전에 만든 문항에는 없다. 그때 화면은 이 줄을 그리지 않는다.
   */
  line?: { usi: string; ja: string; sfen: string }[];
  /**
   * 방금 이 문항에 낸 수와 그 棋譜 표기.
   *
   * `played` 와 다르다. `played` 는 그 판에서 실제로 둔 수다. 둘을 한 필드로 합치면 오답
   * 문구가 사람이 낸 수를 말하지 못한다. 정답과 打 한 글자만 다른 수를 낸 사람은 「내가 그
   * 수를 뒀는데 틀렸다고 한다」고 느낀다(회차 1 #17).
   */
  move: string;
  moveJa?: string;
  /**
   * 낸 수를 둔 뒤의 국면. 없으면 서버가 만들지 못한 것이고, 그때는 문제 국면을 그대로 둔다.
   *
   * 클라이언트는 착수 결과를 계산하지 않고 서버가 보낸 국면을 표시한다(회차 1 #18).
   */
  sfen?: string;
  /** 그 국면에서 王手를 받고 있는 玉의 칸. 낸 수가 王手였으면 상대 玉이다. */
  checked?: string;
  /** 사람이 대국에서 실제로 둔 수. */
  played: string;
  playedJa?: string;
  message: string;
}
