/**
 * 착수음. **파일을 안 쓰고 그 자리에서 만든다.**
 *
 * 레포가 퍼블릭이라 음원을 하나 넣으면 그 파일의 라이선스가 레포의 문제가 된다. 駒가 판에
 * 닿는 소리는 「짧은 충격 + 나무의 울림」이라 합성으로 충분히 가깝고, 그러면 출처를 적을
 * 것도 받아 올 것도 없다 — 06-status.md §70.
 *
 * **AudioContext 를 미리 만들지 않는다.** 브라우저는 사용자가 무엇이든 누르기 전에 만든
 * 컨텍스트를 `suspended` 로 둔다. 첫 착수가 곧 클릭이라 그때 만들면 되고, 그 뒤로는
 * 상대의 수(클릭이 아니다)에도 울린다.
 */

/** 나무의 울림. 두 배음이 서로 조금 어긋나야 「판」이지 「종」이 아니다. */
const PARTIALS = [
  { hz: 880, gain: 0.5, decay: 0.09 },
  { hz: 1370, gain: 0.28, decay: 0.055 },
];

/** 충격음. 이것이 없으면 「똑」이 아니라 「뚱」이 된다. */
const NOISE_MS = 22;
const NOISE_HZ = 2100;
const NOISE_GAIN = 0.45;

/** 전체 크기. 게임 소리는 작게 시작한다 — 크면 한 번 듣고 끄고, 그러면 없는 것과 같다. */
const VOLUME = 0.22;

let ctx: AudioContext | null = null;
let noise: AudioBuffer | null = null;

/**
 * 이 브라우저가 소리를 낼 수 있는가. **없으면 조용히 아무것도 안 한다** — 착수음은
 * 이 제품의 기능이 아니라 감촉이고, 없다고 판을 못 두게 할 이유가 없다.
 */
function audio(): AudioContext | null {
  if (ctx) return ctx;
  const Ctor = window.AudioContext ?? (window as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  ctx = new Ctor();
  return ctx;
}

/** 짧은 백색소음 한 조각. **한 번만 만들어 돌려 쓴다** — 수마다 만들면 착수마다 GC가 돈다. */
function noiseBuffer(c: AudioContext): AudioBuffer {
  if (noise) return noise;
  const frames = Math.ceil((c.sampleRate * NOISE_MS) / 1000);
  const buf = c.createBuffer(1, frames, c.sampleRate);
  const data = buf.getChannelData(0);
  for (let i = 0; i < frames; i++) {
    // 뒤로 갈수록 잦아든다. 그대로 두면 소음이 울림보다 오래 남아 「샤」로 들린다.
    data[i] = (Math.random() * 2 - 1) * (1 - i / frames);
  }
  noise = buf;
  return buf;
}

/**
 * 駒 하나가 판에 닿는 소리.
 *
 * **울리지 못해도 던지지 않는다.** 자동 재생 정책·오디오 장치 없음·탭이 백그라운드 —
 * 전부 정상적인 상황이고, 그때 예외가 화면까지 올라가면 판이 멈춘다.
 */
export function clack(): void {
  const c = audio();
  if (!c) return;

  try {
    // 사용자가 누르기 전에 만들어졌으면 여기서 깨운다. 이미 돌고 있으면 no-op 이다.
    if (c.state === 'suspended') void c.resume();

    const t = c.currentTime;
    const out = c.createGain();
    out.gain.value = VOLUME;
    out.connect(c.destination);

    const burst = c.createBufferSource();
    burst.buffer = noiseBuffer(c);
    const band = c.createBiquadFilter();
    band.type = 'bandpass';
    band.frequency.value = NOISE_HZ;
    band.Q.value = 0.9;
    const burstGain = c.createGain();
    burstGain.gain.setValueAtTime(NOISE_GAIN, t);
    burstGain.gain.exponentialRampToValueAtTime(0.0001, t + NOISE_MS / 1000);
    burst.connect(band).connect(burstGain).connect(out);
    burst.start(t);

    for (const p of PARTIALS) {
      const osc = c.createOscillator();
      osc.type = 'triangle';
      osc.frequency.value = p.hz;
      const g = c.createGain();
      // **setValueAtTime 으로 시작을 못 박는다.** 안 그러면 앞의 수에서 걸어 둔 램프가
      // 이어져, 빨리 두면 소리가 점점 작아진다.
      g.gain.setValueAtTime(p.gain, t);
      g.gain.exponentialRampToValueAtTime(0.0001, t + p.decay);
      osc.connect(g).connect(out);
      osc.start(t);
      osc.stop(t + p.decay);
    }
  } catch {
    // 소리는 감촉이다. 못 내면 조용히 넘어간다.
  }
}
