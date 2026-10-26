// 대국 화면(준비·이어하기·대국·개입 카드·棋譜·총평) 문자열.

export const ja = {
  /** 駒落ち는 先手/後手 대신 下手/上手로 부른다. 사람은 언제나 下手다(`newSetup`). */
  side: { b: '先手', w: '後手', shitate: '下手' },
  handicap: '手合割',
  opponentOpening: '相手の戦型',
  omakase: 'おまかせ',
  moves: (n: number) => `${n}手`,
  left: (n: number) => `残り${n}回`,

  setup: {
    head: '対局のじゅんび',
    hirate: { name: '平手', note: '駒を落とさずに指します' },
    handicapCaveat:
      '駒落ちでは、あなたが下手です。駒を落とした上手（相手）から先に指すので、最初の一手は相手が指します。戦型は選べません。',
    start: '対局をはじめる',
    guide: 'はじめての方へ — このアプリの遊びかた',
    yourSide: 'あなたの手番',
    colorNote: { b: '自分から先に指します', w: '相手の出方を見てから指します' },
    furigoma: { name: '振り駒', note: '部屋をつくったときに決まります' },
    omakaseNote: '相手が自由に指します',
    openingCaveat: '戦型を選ぶと、相手は序盤だけその形に組みます。駒がぶつかってからは自分で考えます。',
    sourceOf: (name: string) => `${name}について`,
    friend: {
      head: '友だちと対局',
      needLogin: '友だちと指すにはログインが必要です。',
      caveat:
        '部屋をつくるとリンクが出ます。それを送った相手が開くと対局がはじまります。持ち時間は一手60秒で、対人戦では口出しもヒントも出ません。',
      creating: '部屋をつくっています…',
      create: '対局部屋をつくる',
    },
  },

  resume: {
    head: '前の対局が残っています',
    turn: '手番',
    moveCount: '進んだ手数',
    caveat: '「はじめから」を選ぶと、この対局は中断のまま終わります。振り返りには出ません。',
    resume: '続きから指す',
    restart: 'はじめから',
  },

  connection: {
    closed: '接続が切れました。',
    restart: 'もう一度はじめる',
    connecting: '接続しています。',
    notConnected: '接続していません。',
    superseded: 'やり直しました。',
    aborted: 'やめました。',
  },

  status: {
    judging: '今の手を確かめています。',
    thinking: '相手が考えています。',
    yourTurn: 'あなたの番です。',
  },

  /**
   * 상대의 강함 눈금에 붙는 말(`snapshot.opponentStrength` 1~5).
   *
   * 3이 「아무것도 모르는 상태」이고 거기서 양쪽으로 움직이는 값이라 이름도 좌우 대칭이다.
   */
  strength: { head: '相手の強さ', levels: ['かなり弱め', '弱め', 'ふつう', '強め', 'かなり強め'] },
  tagHints: '名前のある手があります',

  result: {
    checkmateWon: '詰み。あなたの勝ちです。',
    checkmateLost: '詰み。あなたの負けです。',
    stalemateWon: '手詰まり。あなたの勝ちです。',
    stalemateLost: '手詰まり。あなたの負けです。',
    resignedWon: '相手が投了しました。あなたの勝ちです。',
    resignedLost: '投了しました。',
    repetition: '千日手。引き分けです。',
    aborted: '相手の思考が終わりませんでした。この対局は中断しました。',
  },

  actions: {
    again: 'もう一局',
    analysis: '分析を見る',
    undo: '待った',
    hint: 'ヒント',
    resign: '投了',
    resignConfirmLabel: '投了の確認',
    resignAsk: '投了しますか。',
    resignConfirm: '投了する',
    resignCancel: 'やめる',
  },

  kifu: {
    title: '棋譜',
    copy: 'コピー',
    copied: 'コピーしました',
    empty: '最初の一手を指してください。',
  },

  summary: {
    label: 'この対局のふりかえり',
    head: 'ふりかえり',
    waiting: 'まとめています…',
    playerMoves: 'あなたの手数',
    blunders: '悪手の数',
    undone: '戻した回数',
    accuracy: '精度',
    focusHead: '見直すならこの局面',
    ply: (n: number) => `${n}手目`,
    skill: {
      title: '棋力の目安',
      before: '対局前',
      after: '対局後',
      now: '今の目安',
      note: '指し手の精度から算出した目安です。道場や他のアプリの段級とは異なります。',
    },
  },

  intervention: {
    label: '待った',
    retracted: 'を戻しました',
    mated: 'このあと詰まされます',
    branchTitle: 'そのまま指していたら',
    yourTurn: 'あなたの番',
    theirTurn: '相手の番',
    best: '最善',
    retry: 'もう一度読む',
    back: '一手戻る',
    root: '最初へ',
    drop: (n: number) => `勝率 −${n}%`,
    dismiss: '指し直す',
    note: {
      failed: 'もう一度読み込むと、この手のあとを試せます。',
      pending: '読んでいます…',
      loading: 'この手のあとを調べています。',
      checkmateLost: '詰みです。ここで負けになります。',
      checkmateWon: '詰みです。ここで勝ちになります。',
      stalemateLost: '手詰まりです。ここで負けになります。',
      stalemateWon: '手詰まりです。ここで勝ちになります。',
      yourTurn: 'あなたの番。盤の上でも指せます。',
      theirTurn: '相手の番。相手の手も指せます。',
    },
  },
};

export const ko: typeof ja = {
  side: { b: '선수', w: '후수', shitate: '하수' },
  handicap: '핸디캡',
  opponentOpening: '상대의 전형',
  omakase: '맡기기',
  moves: (n: number) => `${n}수`,
  left: (n: number) => `${n}회 남음`,

  setup: {
    head: '대국 준비',
    hirate: { name: '평수', note: '말을 빼지 않고 둡니다' },
    handicapCaveat:
      '핸디캡 대국에서는 당신이 하수입니다. 말을 뺀 상수(상대)가 먼저 두므로 첫 수는 상대가 둡니다. 전형은 고를 수 없습니다.',
    start: '대국 시작',
    guide: '처음이신 분께 — 이 앱 사용법',
    yourSide: '당신의 차례',
    colorNote: { b: '내가 먼저 둡니다', w: '상대의 수를 보고 나서 둡니다' },
    furigoma: { name: '말 던지기', note: '방을 만들 때 정해집니다' },
    omakaseNote: '상대가 자유롭게 둡니다',
    openingCaveat: '전형을 고르면 상대는 초반에만 그 형태로 짭니다. 말이 부딪친 뒤로는 스스로 생각합니다.',
    sourceOf: (name: string) => `${name}에 대해`,
    friend: {
      head: '친구와 대국',
      needLogin: '친구와 두려면 로그인이 필요합니다.',
      caveat:
        '방을 만들면 링크가 나옵니다. 그 링크를 받은 상대가 열면 대국이 시작됩니다. 제한 시간은 한 수에 60초이며, 대인전에서는 끼어들기도 힌트도 나오지 않습니다.',
      creating: '방을 만드는 중…',
      create: '대국방 만들기',
    },
  },

  resume: {
    head: '이전 대국이 남아 있습니다',
    turn: '차례',
    moveCount: '진행한 수수',
    caveat: '「처음부터」를 고르면 이 대국은 중단된 채로 끝납니다. 복기에는 나오지 않습니다.',
    resume: '이어서 두기',
    restart: '처음부터',
  },

  connection: {
    closed: '연결이 끊겼습니다.',
    restart: '다시 시작',
    connecting: '연결하는 중입니다.',
    notConnected: '연결되어 있지 않습니다.',
    superseded: '다시 요청했습니다.',
    aborted: '취소했습니다.',
  },

  status: {
    judging: '방금 둔 수를 확인하고 있습니다.',
    thinking: '상대가 생각하고 있습니다.',
    yourTurn: '당신 차례입니다.',
  },

  strength: { head: '상대의 강함', levels: ['상당히 약함', '약함', '보통', '강함', '상당히 강함'] },
  tagHints: '이름이 있는 수가 있습니다',

  result: {
    checkmateWon: '외통. 당신의 승리입니다.',
    checkmateLost: '외통. 당신의 패배입니다.',
    stalemateWon: '둘 수가 없음. 당신의 승리입니다.',
    stalemateLost: '둘 수가 없음. 당신의 패배입니다.',
    resignedWon: '상대가 기권했습니다. 당신의 승리입니다.',
    resignedLost: '기권했습니다.',
    repetition: '천일수. 무승부입니다.',
    aborted: '상대의 생각이 끝나지 않았습니다. 이 대국은 중단되었습니다.',
  },

  actions: {
    again: '한 판 더',
    analysis: '분석 보기',
    undo: '무르기',
    hint: '힌트',
    resign: '기권',
    resignConfirmLabel: '기권 확인',
    resignAsk: '기권하겠습니까?',
    resignConfirm: '기권하기',
    resignCancel: '취소',
  },

  kifu: {
    title: '기보',
    copy: '복사',
    copied: '복사했습니다',
    empty: '첫 수를 두세요.',
  },

  summary: {
    label: '이 대국의 복기',
    head: '복기',
    waiting: '정리하는 중…',
    playerMoves: '당신의 수수',
    blunders: '악수 개수',
    undone: '되돌린 횟수',
    accuracy: '정확도',
    focusHead: '다시 본다면 이 국면',
    ply: (n: number) => `${n}수째`,
    skill: {
      title: '기력 가늠',
      before: '대국 전',
      after: '대국 후',
      now: '현재 가늠',
      note: '둔 수의 정확도로 산출한 가늠입니다. 도장이나 다른 앱의 단급과는 다릅니다.',
    },
  },

  intervention: {
    label: '무르기',
    // 앞에 서버가 보낸 棋譜 표기가 붙는다. 조사를 피하려고 「수」로 받는다.
    retracted: ' 수를 되돌렸습니다',
    mated: '이후 외통을 당합니다',
    branchTitle: '그대로 두었다면',
    yourTurn: '당신 차례',
    theirTurn: '상대 차례',
    best: '최선',
    retry: '다시 읽기',
    back: '한 수 뒤로',
    root: '처음으로',
    drop: (n: number) => `승률 −${n}%`,
    dismiss: '다시 두기',
    note: {
      failed: '다시 불러오면 이 수 이후를 시험해 볼 수 있습니다.',
      pending: '읽는 중…',
      loading: '이 수 이후를 살펴보고 있습니다.',
      checkmateLost: '외통입니다. 여기서 패배합니다.',
      checkmateWon: '외통입니다. 여기서 승리합니다.',
      stalemateLost: '둘 수가 없습니다. 여기서 패배합니다.',
      stalemateWon: '둘 수가 없습니다. 여기서 승리합니다.',
      yourTurn: '당신 차례. 판 위에서도 둘 수 있습니다.',
      theirTurn: '상대 차례. 상대의 수도 둘 수 있습니다.',
    },
  },
};
