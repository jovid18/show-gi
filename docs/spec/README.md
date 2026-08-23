# 명세 — 무엇이 어떻게 생겼나

**이 디렉터리는 「형태」만 든다.** 설계 판단과 그 근거는 [`docs/`](../README.md) 본문에 있고, 여기서 다시 쓰지 않는다 — 두 벌이면 한쪽이 조용히 낡고, 그건 틀린 문서보다 나쁘다.

|                                    |                                                                               |
| ---------------------------------- | ----------------------------------------------------------------------------- |
| [api.md](api.md)                   | REST 27개의 요청·응답·상태코드 + **WebSocket 둘의 프로토콜**                  |
| [openapi.yaml](openapi.yaml)       | 위의 REST 부분의 **기계용 정본** (OpenAPI 3.1)                                |
| [data-model.md](data-model.md)     | 표 12개의 **ERD** 와 표 사이의 관계 · 마이그레이션 16개의 이력                |
| [architecture.md](architecture.md) | 그림 여섯 장 — 시스템 구성 · 모듈 의존 · **개입 루프 시퀀스** · 배포 토폴로지 |

## 어디가 정본인가

문서가 코드를 베낀 것이므로, 어긋나면 **언제나 코드가 맞다.** 무엇을 고치면 무엇을 따라 고쳐야 하는지가 아래다.

| 코드를 고치면                                        | 같은 PR 에서 고칠 것                                                |
| ---------------------------------------------------- | ------------------------------------------------------------------- |
| `internal/server/server.go` 의 라우팅                | `openapi.yaml` · `api.md` §2 · `apps/server/README.md` 의 라우트 표 |
| 핸들러의 응답 구조체 (`json:` 태그)                  | `openapi.yaml` 의 `components.schemas` · `api.md` §3                |
| `ws.go` · `ws_match.go` 의 `clientMsg` · `serverMsg` | **`api.md` §5·§6 만** — OpenAPI 는 WebSocket 을 담지 못한다         |
| `store/migrations/` 에 파일을 더하면                 | `data-model.md` §1 ERD · §8 이력 · `02-architecture.md` §4          |
| `internal/game/session.go` 의 판정·롤백 순서         | `architecture.md` §3.2 시퀀스                                       |
| 모듈을 하나 더하거나 의존을 늘리면                   | `architecture.md` §2 — **없는 화살표가 설계다**                     |

**「없는 것」을 적어 둔 자리 셋을 지우지 않는다.** [api.md §7](api.md)(이 API 에 없는 것) · [architecture.md §2](architecture.md)(없는 화살표 여덟) · [architecture.md §6](architecture.md)(무엇이 없으면 무엇이 꺼지나). 있는 줄 알고 찾는 시간이 없는 것을 읽는 시간보다 비싸다.

## 읽는 순서

처음 열었다면 [architecture.md §3](architecture.md#3-개입-루프--이-제품의-코어) 하나만 봐도 된다. **한 수가 물러지기까지**가 이 제품의 전부다.

그다음은 목적에 따라 갈린다.

- **API 를 붙일 것이다** → [api.md §1](api.md) 의 규약 둘 → §2 라우트 표 → 필요한 스키마
- **DB 를 만질 것이다** → [data-model.md §2](data-model.md) 의 세 덩어리 → §3 (한 手数가 여러 표에 있다)
- **왜 이렇게 만들었나가 궁금하다** → 여기가 아니라 [01-core](../01-core.md) · [02-architecture](../02-architecture.md)
