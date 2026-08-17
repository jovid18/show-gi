import { useCallback, useEffect, useRef, useState } from 'react';

import type { ClientMessage, Color, GameSummary, ServerMessage, Snapshot } from '@/protocol/game';
import type { WhatIfNode, WhatIfRequest } from '@/protocol/whatif';
import type { ResumableGame } from '@/protocol/resume';
import type { Send } from '@/hooks/useWhatIf';

export type Connection = 'idle' | 'connecting' | 'open' | 'closed';

/**
 * 시작 화면이 고른 것. **대국이 열리기 전에 정해지고 그 판 동안 안 바뀐다.**
 *
 * 서버는 이걸 WS 주소의 쿼리로 받는다(`internal/server/ws.go` 의 `newSetup`) — `start`
 * 메시지로 보내지 않는 이유는 그쪽 주석에 있다.
 */
export interface GameSetup {
  /** 사람이 잡을 쪽. */
  color: Color;
  /** 상대가 따를 진형의 id. 「おまかせ」면 null. */
  opening: string | null;
}

export interface GameState {
  connection: Connection;
  /**
   * 마지막으로 고른 설정. **대국이 끝나도 남는다** — 시작 화면이 이 값에서 시작하므로
   * 같은 조건으로 또 두는 것이 버튼 한 번이다. 아직 한 판도 안 열었으면 null.
   */
  setup: GameSetup | null;
  snapshot: Snapshot | null;
  /** 서버가 착수를 거절한 이유. 일본어 문구가 그대로 온다. */
  rejection: string | null;
  /**
   * 대국이 끝난 뒤의 총평. **결과 문구보다 늦게 온다** — 기록이 다 쓰이기를 기다린다.
   *
   * `null` 인 동안이 「아직 만들고 있다」이고, 화면은 그때 자리만 잡아 둔다. 안 오는
   * 경우도 있다(기록이 없는 배포) — 그때도 결과와 기보는 이미 화면에 있다.
   */
  summary: GameSummary | null;
  /**
   * 개입 **회차**. 개입이 실려 온 스냅샷마다 하나씩 오른다.
   *
   * `snapshot.intervention` 이 있는지만 보면 안 된다 — 서버는 다음 착수까지 그걸 들고
   * 있으므로, 같은 자리에서 같은 수로 또 걸렸을 때 화면이 "아까 그거"로 착각한다.
   * 회차로 세면 연출을 다시 돌릴지가 명확해진다.
   */
  interventionEpisode: number;
  play: (usi: string) => void;
  resign: () => void;
  /**
   * 직전 자기 수를 무른다(待った).
   *
   * **무엇을 되돌릴지 안 보낸다.** 누를 수 있는지는 `snapshot.canUndo` 가 이미 답했고,
   * 거절되면 다른 거절과 같은 자리에 문구가 뜬다(`rejection`).
   */
  undo: () => void;
  /**
   * 최선수 힌트를 부른다. **누를 수 있는지는 `snapshot.canHint` 가 이미 답했다** —
   * 예산·차례·이 국면에서 답을 봤는지 셋을 화면이 다시 짓지 않는다(`undo` 와 같은 규약).
   */
  askHint: () => void;
  dismissRejection: () => void;
  /** 고른 설정으로 대국을 연다. 새 연결이 곧 새 판이다. */
  start: (setup: GameSetup) => void;
  /**
   * 중단된 판을 이어서 연다.
   *
   * **새 판을 여는 것과 같은 연결이다.** 서버가 기보에서 국면을 다시 세우므로
   * (`journal` §46), 화면 쪽에서 갈리는 것은 주소의 쿼리 하나뿐이다.
   */
  resume: (game: ResumableGame) => void;
  /**
   * 대국을 접고 시작 화면으로 돌아간다.
   *
   * **바로 다시 붙지 않는다.** 다음 판에서 선후공이나 진형을 바꾸고 싶은 것이 보통이고,
   * 같은 설정으로 또 두는 것은 시작 화면에서 버튼 한 번이다.
   */
  restart: () => void;
  /**
   * 가정 수순 한 자리를 **이 대국의 연결로** 묻는다(`useWhatIf` 의 `Send`).
   *
   * **되짚기와 길이 갈리는 이유는 뿌리다.** 저쪽은 DB 기록에서 만들지만, 두는 중인 판은
   * 기록이 비동기로 쌓여서 개입 직후에는 마지막 수가 아직 없을 수 있다 — 하필 제일
   * 누르고 싶은 순간에 흔들린다. 세션이 방금 보낸 스냅샷이 그 자리의 정본이다.
   */
  whatif: Send;
}

/**
 * 이 연결이 여는 주소.
 *
 * **이어할 때는 색도 진형도 안 보낸다.** 서버가 그 판의 행에서 읽는다(`ws.go`) — 클라이언트가
 * 되보내면 「기록에 남은 판」과 「화면이 기억하는 판」이 갈리는 자리가 하나 생기고, 어긋나면
 * 이어한 판이 그때 두던 판이 아니게 된다.
 */
function socketUrl(setup: GameSetup, resumeId: number | null): string {
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
  const query =
    resumeId !== null ? new URLSearchParams({ resume: String(resumeId) }) : new URLSearchParams({ color: setup.color });
  if (resumeId === null && setup.opening) query.set('opening', setup.opening);
  return `${scheme}://${window.location.host}/ws/game?${query}`;
}

/**
 * `/ws/game`에 붙어 대국 하나를 연다.
 *
 * 스냅샷은 **항상 전체 상태**라 여기서 이전 것과 합치지 않는다. 받은 것으로 통째로 바꾼다 —
 * 부분 갱신을 재구성하기 시작하면 D3의 롤백 뒤에 화면과 서버가 어긋나도 알 방법이 없다.
 */
export function useGame(): GameState {
  const [connection, setConnection] = useState<Connection>('idle');
  const [setup, setSetup] = useState<GameSetup | null>(null);
  // 판이 열려 있는가. **setup 과 갈라 둔다** — setup 은 다음 판의 기본값으로 남아야 하고,
  // 그것으로 「지금 두는 중인가」를 겸하면 대국을 접는 순간 고른 것도 같이 사라진다.
  const [live, setLive] = useState(false);
  // 이어하는 판의 번호. **setup 과 갈라 둔다** — setup 은 다음 판의 기본값으로 남지만
  // 이 값은 그 연결 하나에만 산다. 섞어 두면 「もう一局」이 끝난 판을 또 이어하려 든다.
  const [resumeId, setResumeId] = useState<number | null>(null);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [rejection, setRejection] = useState<string | null>(null);
  const [interventionEpisode, setInterventionEpisode] = useState(0);
  const [summary, setSummary] = useState<GameSummary | null>(null);
  // 새 대국은 새 연결이다. 서버가 연결 하나에 대국 하나를 여니, 다시 붙는 것이 곧 새 판이다.
  const [generation, setGeneration] = useState(0);
  const socketRef = useRef<WebSocket | null>(null);
  // 직전 스냅샷에 개입이 실려 있었는가. 회차를 세는 데만 쓴다.
  const hadIntervention = useRef(false);

  /**
   * 답을 기다리는 가정 수순 요청.
   *
   * **하나뿐이다.** 서버가 한 연결에 한 번만 돌리므로(ws.go 의 슬롯) 여기도 하나면 되고,
   * 새 요청이 오면 앞의 것은 버려진다 — 그쪽은 이미 `useWhatIf` 가 abort로 접은 것이다.
   */
  const asking = useRef<{ resolve: (n: WhatIfNode) => void; reject: (e: Error) => void } | null>(null);
  const settle = useCallback((fn: (p: NonNullable<typeof asking.current>) => void) => {
    const pending = asking.current;
    if (!pending) return;
    asking.current = null;
    fn(pending);
  }, []);

  useEffect(() => {
    // 고르기 전에는 붙지 않는다. **여기서 미리 붙으면 그 순간 판이 하나 열려** 기록에
    // 남고, 사람이 아직 아무것도 고르지 않은 채로 先手 평수 대국이 시작된다.
    if (!live || !setup) return;

    const socket = new WebSocket(socketUrl(setup, resumeId));
    socketRef.current = socket;
    hadIntervention.current = false;

    /**
     * 이 소켓이 아직 **지금 대국의 것**인가.
     *
     * 정리에서 `socket.close()` 를 부르면 그 `close` 이벤트가 뒤늦게 도착해 방금 정한
     * 상태를 덮는다. 「もう一局」이 시작 화면이 아니라 **「接続が切れました」**로 가던
     * 것이 이것이었다 — 우리가 일부러 닫은 것을 사고로 보고하고 있었다.
     */
    let current = true;
    const dropped = (): void => {
      if (current) setConnection('closed');
    };

    socket.addEventListener('open', () => {
      if (current) setConnection('open');
    });
    socket.addEventListener('close', dropped);
    socket.addEventListener('error', dropped);

    socket.addEventListener('message', (event) => {
      let msg: ServerMessage;
      try {
        msg = JSON.parse(String(event.data)) as ServerMessage;
      } catch {
        return; // 우리가 못 읽는 것은 무시한다. 판을 지우는 것보다 낫다
      }
      if (msg.type === 'snapshot') {
        // 서버는 착수 하나에 개입 하나를 싣고 다음 착수까지 들고 있는다. 그래서 "있다"가
        // 아니라 **없다가 생긴 순간**이 새 개입이다.
        const has = Boolean(msg.snapshot.intervention);
        if (has && !hadIntervention.current) setInterventionEpisode((n) => n + 1);
        hadIntervention.current = has;

        setSnapshot(msg.snapshot);
        setRejection(null);
      } else if (msg.type === 'summary') {
        setSummary(msg.summary);
      } else if (msg.type === 'error') {
        setRejection(msg.message);
      } else if (msg.type === 'whatif') {
        settle((p) => p.resolve(msg.whatif));
      } else if (msg.type === 'whatif_error') {
        // **착수 거절과 갈라 둔다.** 저쪽은 판 위의 실패라 판 옆에 뜨고, 이쪽은 가정 수순
        // 패널 안의 실패다 — 한 자리에 뭉치면 「두다가 뭘 잘못했나」로 읽힌다.
        settle((p) => p.reject(new Error(msg.message)));
      }
    });

    return () => {
      current = false;
      socketRef.current = null;
      // 끊긴 연결에는 답이 안 온다. 기다리는 쪽을 영원히 매달아 두지 않는다.
      settle((p) => p.reject(new Error('接続が切れました。')));
      socket.close();
    };
  }, [generation, live, setup, resumeId, settle]);

  const send = useCallback((msg: ClientMessage) => {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
  }, []);

  const play = useCallback((usi: string) => send({ type: 'move', usi }), [send]);
  const resign = useCallback(() => send({ type: 'resign' }), [send]);
  const undo = useCallback(() => send({ type: 'undo' }), [send]);
  const askHint = useCallback(() => send({ type: 'hint' }), [send]);
  const dismissRejection = useCallback(() => setRejection(null), []);

  const whatif = useCallback<Send>(
    (req: WhatIfRequest, signal: AbortSignal) =>
      new Promise<WhatIfNode>((resolve, reject) => {
        const socket = socketRef.current;
        if (socket?.readyState !== WebSocket.OPEN) {
          reject(new Error('接続していません。'));
          return;
        }
        // 앞의 것이 아직 매달려 있으면 접는다. 답은 하나만 온다.
        settle((p) => p.reject(new Error('やり直しました。')));
        asking.current = { resolve, reject };
        signal.addEventListener('abort', () => settle((p) => p.reject(new Error('やめました。'))), { once: true });
        socket.send(JSON.stringify({ type: 'whatif', ply: req.ply, moves: req.moves } satisfies ClientMessage));
      }),
    [settle],
  );

  const start = useCallback((next: GameSetup) => {
    // 판을 먼저 비운다. 끝난 대국이 남아 있으면 새 판이 오기 전 한 순간 그게 보인다.
    setSnapshot(null);
    setRejection(null);
    setSummary(null);
    setConnection('connecting');
    setSetup(next);
    setResumeId(null);
    setLive(true);
    // 같은 설정으로 또 두는 것도 **새 연결이어야 한다** — setup 이 그대로면 효과가 다시
    // 돌지 않으므로 세대를 올려 준다.
    setGeneration((n) => n + 1);
  }, []);

  const resume = useCallback((game: ResumableGame) => {
    setSnapshot(null);
    setRejection(null);
    setSummary(null);
    setConnection('connecting');
    // **그 판의 조건을 다음 판의 기본값으로도 삼는다.** 이어한 판이 끝나고 「もう一局」을
    // 누르면 같은 선후공·진형에서 시작하는 것이 자연스럽다.
    setSetup({ color: game.myColor, opening: game.opening ?? null });
    setResumeId(game.id);
    setLive(true);
    setGeneration((n) => n + 1);
  }, []);

  const restart = useCallback(() => {
    setSnapshot(null);
    setRejection(null);
    setSummary(null);
    setConnection('idle');
    // setup 은 지우지 않는다 — 시작 화면이 그 값에서 시작한다(GameState.setup).
    setLive(false);
    // **이어하기는 지운다.** 이 판은 이제 그 판이 아니다.
    setResumeId(null);
  }, []);

  return {
    connection,
    setup,
    snapshot,
    rejection,
    summary,
    interventionEpisode,
    play,
    resign,
    undo,
    askHint,
    dismissRejection,
    start,
    resume,
    restart,
    whatif,
  };
}
