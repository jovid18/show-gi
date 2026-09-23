// 되짚기 퀴즈 계약이다. 서버 쪽 짝은 `internal/server/quiz.go` 다.
//
// 문항에는 정답이 없다. 채점은 서버가 하고, 정답은 채점 응답에서 처음 온다. 문항에 담으면
// 화면을 여는 순간 답을 알 수 있다.

/** 문항 전체다. 엔드포인트는 `GET /api/games/{id}/quiz` 다. */
export interface QuizPayload {
  /** 생성이 끝났는지다. `false` 는 「아직 생성 중」이고 「문항 없음」과 다르다(`useQuiz`). */
  ready: boolean;
  /**
   * 문항이 오기를 기다리는 중인지다. `ready` 가 false 일 때만 의미가 있고, ready·queued 가 둘
   * 다 false 면 화면은 기다리기를 멈춘다.
   *
   * 큐 상태만 나타내지 않는다. 가져온 판은 모든 수를 평가한 뒤에 문항을 큐에 넣으므로,
   * 평가하는 동안에도 true 다. 가져오기 직후 열람하면 이 경우다.
   *
   * 배포 중에는 옛 태스크가 이 필드 없이 응답할 수 있다. 필드가 없으면 false 가 아니라 「알려
   * 주지 않았다」로 읽는다.
   */
  queued?: boolean;
  mate?: MateItem;
  best?: BestItem[];
}

/** 詰み 문항이다. */
export interface MateItem {
  /** 문제 국면이 생긴 手数다. 사람은 `ply+1` 手目을 둘 차례다. */
  ply: number;
  sfen: string;
  /** 詰みまでの手数다. 手数를 모르면 詰将棋를 풀 수 없어서 문항에 담는다. */
  plies: number;
  /** 사람이 대국에서 이 詰み을 실제로 성공시킨 판인지다. 제목을 나누는 기준이다. */
  converted: boolean;
  /**
   * 王手인 수만 담는다. 詰将棋의 攻方은 매 수 王手를 걸어야 하므로 다른 수는 입력에서 뺀다.
   *
   * 화면은 이 배열만 하이라이트하므로 王手가 아닌 수를 보낼 수 없다.
   */
  legalMoves: string[];
  /**
   * 王手를 받는 玉의 칸이다. 王手를 풀면서 거는 수만 남은 국면에서는 詰ます 쪽 자신도 王手를
   * 받는다.
   */
  checked?: string;
}

/** 「この局面の最善手は?」 문항이다. */
export interface BestItem {
  /** 채점 요청에서 문항을 가리키는 값이다. 手数 대신 목록 안 위치를 쓴다. */
  index: number;
  ply: number;
  sfen: string;
  /** 합법수 전체다. 王手 한정 규약은 詰み 문항에만 적용한다. */
  legalMoves: string[];
  checked?: string;
}

/** 詰み 문항 채점 요청이다. 玉方 응수는 보내지 않는다. 서버가 트리에서 꺼내 둔다. */
export interface MateAttempt {
  moves: string[];
  /**
   * 이 문항의 시도 번호이고 1부터 센다. 화면이 세고 서버는 저장하지 않는다.
   *
   * 서버가 저장하면 되짚기를 다시 열 때마다 「이미 세 번 틀린 문항」으로 시작한다. 큰 값을
   * 보내도 `hint` 만 돌아오므로 이 값으로 정답을 얻을 수 없다.
   */
  attempt: number;
}

/** 마지막 수의 결과 상태다. */
export type MateOutcome = 'ongoing' | 'solved' | 'wrong' | 'not_check';

/** 詰み 문항 채점 결과이자 다음 장면이다. */
export interface MateResult {
  /** 판 위에서 진행된 수 전체다. 내 수와 玉方 응수가 번갈아 온다. */
  line: string[];
  sfen: string;
  /** 직전 내 수에 대한 玉方 응수다. 화면이 이 수를 표시해야 무엇이 달라졌는지 보인다. */
  defense?: string;
  defenseJa?: string;
  /** 다음에 둘 수 있는 王手 목록이다. 끝나면 오지 않는다. */
  legalMoves?: string[];
  checked?: string;
  /** 현재 국면의 詰みまでの手数다. 끝나면 0이다. */
  plies: number;
  outcome: MateOutcome;
  /** 서버가 만든 일본어이고 화면에 그대로 표시한다. */
  message: string;
  /**
   * 「무엇을 어디서 움직이나」다(「7九の銀」). 세 번째 오답에서만 오고, 이 응답에는 정답 수가
   * 전혀 없다.
   *
   * 첫 오답에 정답을 담으면 한 번 틀리는 순간 문항이 끝난다(회차 2 #10 · #11).
   */
  hint?: string;
}

/** 「최선수는?」 문항 채점 요청이다. */
export interface BestAttempt {
  index: number;
  move: string;
  /** `MateAttempt.attempt` 와 같은 규약이다. */
  attempt: number;
}

/** 「최선수는?」 문항 채점 결과다. */
export interface BestResult {
  correct: boolean;
  /** 정답과 cp 두 개는 정답을 맞혔을 때만 온다(회차 2 #10 · #11). */
  answer?: string;
  answerJa?: string;
  /** 사람 관점 cp 다. 두 값의 차가 이 문항을 고른 기준이다. */
  answerCp?: number;
  secondCp?: number;
  /** `MateResult.hint` 와 같은 규약이다. */
  hint?: string;
  /**
   * 정답 뒤 양쪽이 최선으로 진행한 수순이다. 맞혔을 때만 온다. 첫 수가 정답이라 이것만으로
   * 정답을 알려 준 것과 같다.
   *
   * 이 필드가 생기기 전에 만든 문항에는 영영 없다. 화면은 그 줄을 표시하지 않는다.
   */
  line?: { usi: string; ja: string; sfen: string }[];
  /**
   * 방금 이 문항에 낸 수와 그 棋譜 표기다. `played` 는 그 판에서 실제로 둔 수이고 이 값과
   * 다르다.
   *
   * 둘을 합치면 오답 문구가 낸 수를 말하지 않는다. 정답과 打 한 글자만 다른 수를 낸
   * 사람에게는 「내가 그것을 뒀는데 틀렸다고 한다」로 들린다(회차 1 #17).
   */
  move: string;
  moveJa?: string;
  /**
   * 낸 수를 둔 뒤의 국면이다. 없으면 서버가 만들지 못한 것이고 화면은 문제 국면을 그대로
   * 둔다.
   *
   * 클라이언트는 착수 결과를 계산하지 않고 서버가 돌려준 국면을 표시한다(회차 1 #18).
   */
  sfen?: string;
  /** 그 국면에서 王手를 받는 玉의 칸이다. 낸 수가 王手면 상대 玉이다. */
  checked?: string;
  played: string;
  playedJa?: string;
  message: string;
}
