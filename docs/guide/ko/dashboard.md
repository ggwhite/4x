# 4x Live 대시보드

AI 개발 루프의 실시간 모니터링.

## 대시보드 시작

```bash
# 최근 프로젝트로 시작
4x live

# 특정 프로젝트 열기
4x live /path/to/project1 /path/to/project2

# 커스텀 포트
4x live -p 8080

# 브라우저에서 자동 열기
4x live -w

# macOS 네이티브 앱 열기
4x live -a
```

## 다중 프로젝트 지원

대시보드는 여러 프로젝트를 동시에 지원합니다. 경로 인수 없이 실행하면 `~/.4x/recent-projects.json`에서 로드합니다(LRU, 최대 20개 항목).

프로젝트 탭 바 끝에는 **프로젝트 추가**(폴더 플러스 아이콘)와 **전역 설정**(톱니바퀴 아이콘) 두 가지 액션이 있습니다. 사이드바 헤더에는 활성 프로젝트의 **프로젝트 설정** 톱니바퀴와 그 옆에 **정리** 버튼(휴지통 아이콘)이 있습니다. 정리를 클릭하면 정리된 기능의 상세 로그, 보고서, 라운드 히스토리가 대시보드에서 손실된다는 경고를 포함한 확인 대화상자가 열립니다(기능 정의와 상태는 보존됨). 확인하면 전체 프로젝트에 대해 [`POST /api/clean`](#post-apiclean)을 호출하고 결과를 토스트로 표시합니다.

## 기능 카드

각 기능 카드에는 우선순위, 의존성, 중지 사유(기능이 비정상 종료된 경우), 그리고 기본이 아닌 [파이프라인 프로파일](concepts.md#파이프라인-프로파일)이 활성화된 경우 **프로파일 태그**(예: `quick`, `normal`)가 표시됩니다. 높은 우선순위 기능(P0/P1)은 강조 테두리가 적용됩니다. 완료된 의존성은 녹색 체크 마크로 표시됩니다. `profile`, `stopReason`, `stopMessage` 필드는 `/api/tasks` JSON에 포함됩니다. `stopReason`은 짧은 카테고리 코드(예: `runner-error`, `guard-fail`, `no-progress`)로 색상 구분에 사용되며, `stopMessage`는 카테고리 라벨 아래에 표시되는 사람이 읽을 수 있는 상세 설명입니다.

## 새 기능 폼

**새 기능** 모달은 단계적 폼입니다. 기본 영역에는 항상 **이름**(필수), **설명**(선택, 기본값은 이름), **우선순위** 선택(P0-P3 또는 없음)이 표시됩니다. **고급** 토글을 열면 **커스텀 ID**(비워두면 자동 생성), **의존성**(쉼표로 구분된 기능 ID), **규칙**(쉼표로 구분), 동적 **하위 작업** 목록(id + name 행 추가/제거)이 표시됩니다. 제출하면 [`/api/new`](#rest)에 `POST`합니다. CLI `4x new`와 대시보드는 단일 생성 경로(`feature.Create`, [개념](concepts.md#기능-생성) 참조)를 공유하므로, 둘 다 동일한 플래그/필드와 ID 생성을 따릅니다.

## 의존성 DAG

개요에서는 모든 기능의 의존성 그래프를 인라인 SVG로 렌더링합니다 — 외부 차트 라이브러리(d3, mermaid, chart.js)는 로드하지 않습니다. 기능은 의존성 깊이에 따라 레이어로 배치되며, 엣지는 각 기능에서 의존하는 기능으로 연결됩니다. 노드 색상은 단계 상태를 따릅니다: 녹색 = done, 파란색 = 실행 중(활성 실행 또는 coding/reviewing/testing 같은 진행 중 단계), 회색 = todo, 빨간색 = blocked / needs-attention. 노드를 클릭하면 기능 카드를 클릭하는 것과 같은 경로로 해당 기능의 상세 페이지가 열립니다. 그래프는 캐시된 `/api/tasks` 데이터에서 매 폴링 주기마다 재구성되므로, 기능이 진행됨에 따라 색상이 실시간으로 업데이트됩니다.

## 배치 패널

개요에는 [배치 제어 API](#배치-제어)로 지원되는 배치 제어 패널도 있습니다. **배치 시작 / 중지 / 계속** 버튼(시작은 실행 전에 확인 요청), 실행 중 표시기, 기능별 진행 상태(완료 체크, 실행 중 마커, 대기 위치)가 포함된 예정 대기열, 그리고 병합 충돌로 배치가 일시정지된 경우 기능, 리포, 충돌 파일과 함께 배치 계속 액션이 있는 충돌 카드가 표시됩니다. 패널은 대시보드의 나머지와 같은 폴링 루프에서 `GET /api/batch/status`로 갱신됩니다.

## 서버 API

대시보드는 REST 및 SSE 엔드포인트를 노출합니다:

읽기 중심 엔드포인트(`/api/tasks`, `/api/overview`, `/api/projects`, `/api/settings` 등)는 일반 `*protocol.Workspace` 대신 `*protocol.CachedWorkspace`를 통해 서비스됩니다. 서버가 장기 실행되기 때문에, 이 mtime 기반 인메모리 캐시는 요청마다 모든 기능 YAML과 `settings.json`을 재파싱하는 것을 방지합니다 — [워크스페이스 읽기 캐시](concepts.md#워크스페이스-읽기-캐시-대시보드-서버)를 참조하세요. 캐시 무효화는 자동입니다: 쓰기(대시보드 또는 러너를 통한)가 파일 mtime을 변경하면 다음 읽기에서 투명하게 재파싱됩니다.

### REST

| 엔드포인트 | 메서드 | 설명 |
|---|---|---|
| `/api/tasks` | GET | 모든 기능 나열 (기능 YAML에 형식 문제가 있을 때 `warnings` 배열 포함) |
| `/api/new` | POST | 새 기능 생성 (`name`, `description` 및 선택적 `customId`, `priority`, `depends`, `rules`, `subtasks` 허용) |
| `/api/run` | POST | 기능 실행 시작 (`4x run` 서브프로세스 생성) |
| `/api/stop` | POST | 실행 중인 기능 중지 |
| `/api/done` | POST | 기능을 완료로 표시; worktree가 있으면 자동 병합 (멀티 리포: 전부 성공 또는 전부 실패) |
| `/api/clean` | POST | 프로젝트 내 모든 정리 가능(done/abandoned) 기능의 워크스페이스 아티팩트 제거 |
| `/api/runs` | GET | 활성 실행 나열 |
| `/api/batch/start` | POST | 배치 실행 시작 (`4x batch run` 서브프로세스); 미해결 배치 충돌 시 409 |
| `/api/batch/stop` | POST | 배치 정상 중지 (`.4x/batch-stop` 기록) |
| `/api/batch/continue` | POST | 충돌 신호 지우고 배치 재시작 (worktree에서 충돌 해결 후) |
| `/api/batch/status` | GET | 배치 실행 상태, 예정 대기열, 현재 기능, 충돌 신호 |
| `/api/events/{id}` | GET | 기능의 이벤트 가져오기 |
| `/api/overview/{id}` | GET | 기능 개요 가져오기 (YAML 필드 + spec/plan 내용, 공유 `protocol.ResolveDesignDoc`를 통해 해석 — [설계 문서 해석](concepts.md#설계-문서-해석) 참조) |
| `/api/messages/{id}` | GET | 기능의 메시지 가져오기 |
| `/api/evolve-report` | GET | 최신 `4x evolve` 라운드 요약(`.4x/evolve-report.md`); `{content, exists}`, 존재하지 않을 때 `exists:false` |
| `/api/features/{id}/screenshots` | GET | 라운드별 그룹화된 스크린샷 가져오기 |
| `/api/features/{id}/screenshots/{filename}` | GET | 스크린샷 이미지 하나 서빙 |
| `/api/logs/{id}` | GET | 기능의 로그 파일 나열 |
| `/api/logs/{id}/{file}` | GET | 특정 로그 파일 가져오기 |
| `/api/projects` | GET | 등록된 프로젝트 나열 |
| `/api/projects` | POST | 프로젝트 추가 (즉석 초기화를 위한 `init: true` 지원) |
| `/api/projects/{id}` | DELETE | 프로젝트 제거 |
| `/api/browse` | GET | 폴더 선택기 |
| `/api/settings` | GET | 프로젝트 설정 (`.4x/settings.json`) 가져오기 |
| `/api/settings` | PUT | 프로젝트 설정 업데이트 (검증, 백업, 기록) |
| `/api/user-config` | GET | 사용자 설정 (`~/.4x/settings.json`) 가져오기 |
| `/api/user-config` | PUT | 사용자 설정 업데이트 (`.bak`에 백업 후 기록) |
| `/api/merged-config` | GET | 프로젝트 + 사용자 병합된 유효 설정의 읽기 전용 뷰 |
| `/api/locales` | GET | 지원되는 로케일 목록 반환 |
| `/api/locales/{lang}` | GET | 해당 언어의 번역 JSON 반환 |
| `/api/supported-runners` | GET | 지원되는 러너 이름 나열 |

#### `POST /api/done` 응답

일반적인 경우 HTTP 200을 반환합니다. `status` 필드는 상태 전환이 성공한 후에만 `"done"`입니다. 병합 충돌 또는 병합 오류가 발생하면 `status`는 `"pending-review"`로 유지됩니다. 추가 필드로 병합 결과를 나타냅니다:

| 필드 | 타입 | 의미 |
|---|---|---|
| `merged` | bool | `true`면 브랜치가 병합되고 worktree가 정리됨 |
| `merged` | bool | `false`면 worktree가 없었음 (상태만 전환) |
| `merge_conflict` | bool | `true`면 병합 충돌 발생; worktree 보존됨 |
| `merge_error` | string | 병합 오류 메시지; 기능은 pending-review 유지 |
| `conflicts` | string[] | 충돌 파일 목록 (`merge_conflict: true`일 때만 존재) |

충돌 후, worktree에서 파일을 해결하고 `4x merge <id>`를 실행하여 완료하세요.

병합 중 기능의 단계가 변경되면(러너 또는 백그라운드 리콘실러가 병합 실행 중 `state.json`을 업데이트한 경우), 엔드포인트는 **HTTP 409 Conflict**를 `{"status":"<currentPhase>","error":"state changed during merge"}`와 함께 반환하고 done 전환을 수행하지 않습니다 — 이는 최신 상태를 부실한 병합 전 스냅샷으로 덮어쓰는 것을 방지합니다.

#### `POST /api/clean`

프로젝트 내 **모든** 정리 가능한 기능의 `.4x/{feature-id}/` 워크스페이스 아티팩트(로그, `rounds/`, 보고서, `state.json`, `events.jsonl`)를 한 번의 호출로 제거합니다 — `4x clean`이 정리할 것과 동일한 세트: `done`/`abandoned`, 비활성, 워크스페이스 디렉토리 존재. 기능 정의(`.4x/features/*.yaml`)는 보존되므로, 정리된 기능도 최종 상태와 함께 목록에 계속 표시됩니다. 기반 프로토콜 함수에 대해서는 [워크스페이스 정리](concepts.md#workspace-cleanup)를 참조하세요.

비 `POST` 요청은 **HTTP 405**를 반환합니다. 각 기능은 독립적으로 정리됩니다; 실패(예: 경합으로 활성 상태가 된 경우)하면 해당 기능만 건너뛰고 나머지는 계속됩니다. 핸들러는 항상 HTTP 200을 반환합니다:

| 필드 | 타입 | 의미 |
|---|---|---|
| `cleaned` | int | 아티팩트가 제거된 기능 수 |
| `freed` | int64 | 해제된 총 바이트 |
| `freed_human` | string | `freed`를 사람이 읽기 쉬운 형식으로 표시 (예: `38M`) |
| `features` | string[] | 정리된 기능의 ID (정리할 것이 없으면 `[]`) |

정리할 것이 없으면 응답은 `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`입니다.

#### 배치 제어

대시보드는 터미널로 돌아가지 않고도 배치 실행을 처음부터 끝까지 제어할 수 있습니다. 전용 `BatchManager`(기능별 `ProcessManager`와 별도)가 프로젝트의 단일 `4x batch run` 서브프로세스를 관리합니다 — 한 번에 하나의 배치만 실행 가능합니다.

- **시작** (`POST /api/batch/start`) — UI가 먼저 확인하여 실수로 실행하는 것을 방지한 후 실행을 시작합니다. `.4x/batch-conflict.json`이 아직 존재하면 엔드포인트가 **HTTP 409**를 반환하므로 부실 충돌을 먼저 해결하거나 계속해야 합니다. 요청 본문에 `{runner, maxRounds}`를 담을 수 있으며, 생략된 필드는 병합된 프로젝트/사용자 설정으로 폴백합니다.
- **중지** (`POST /api/batch/stop`) — 정상 중지를 위해 `.4x/batch-stop`을 기록합니다(배치가 현재 기능을 완료한 후 종료). 서브프로세스를 **kill하지 않습니다**.
- **계속** (`POST /api/batch/continue`) — `.4x/batch-conflict.json`을 지우고 배치를 재시작합니다. worktree에서 충돌을 해결한 후 사용합니다.
- **상태** (`GET /api/batch/status`) — 실행 플래그, 예정 대기열, 현재 기능, 충돌 신호(또는 `null`), `lastReport`(파싱된 `.4x/batch-report.json` 또는 보고서가 없으면 생략)를 반환합니다:

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  대기열은 `batch.PlanBatch`에서 구성되므로 CLI와 동일한 의존성 및 우선순위 순서를 따릅니다. 각 항목의 `state`는 `done`(기능 완료 / ready-for-review), `running`(완료되지 않은 활성 실행), `error`(blocked / needs-attention), 또는 `waiting`이며, `position`은 미완료 항목에 번호를 매깁니다(`done`과 `error` 제외).

  `lastReport`는 가장 최근 배치 실행의 보고서(`outcome`, 카운트, 러너, 소요 시간, 기능별 분석 — [배치 모드](batch.md#run-report) 참조)를 담습니다. 배치가 실행 중이 아닐 때, 패널은 이를 기능별 상세로 펼칠 수 있는 "마지막 배치 보고서" 요약 카드로 렌더링합니다; `crashed` 결과의 경우 `panicMessage`도 표시합니다.

### 스크린샷 탭

기능 상세에는 해당 기능에 스크린샷이 존재할 때 **스크린샷** 탭이 포함됩니다. 스크린샷은 라운드별로 그룹화되어 썸네일로 표시되며, 라이트박스에서 좌/우 탐색과 ESC로 닫기가 가능합니다.

### SSE (Server-Sent Events)

| 엔드포인트 | 설명 |
|---|---|
| `/sse/events/{id}` | 기능의 이벤트 스트리밍 (1초 폴링) |
| `/sse/logs/{id}` | 기능의 활성 로그 파일 스트리밍 (하나 이상) |

이벤트 스트림은 `events.jsonl`의 바이트 오프셋을 추적하며 새로 추가된 줄만 전송합니다. 파일이 **잘리거나 로테이션**되면 — 예를 들어 `4x transition --to init`이 기능을 초기화하고 `events.jsonl`을 처음부터 다시 쓰면 — 새 파일 크기가 추적된 오프셋 아래로 떨어집니다. 스트림은 이를 감지(`size < lastOffset`)하고 오프셋을 0으로 재설정하여 파일 전체를 처음부터 다시 읽으므로 클라이언트가 영원히 멈추지 않고 복구합니다. 크기가 오프셋과 같으면 "새 내용 없음"을 의미하며 건너뜁니다.

로그 스트림(`/sse/logs/{id}`)도 마찬가지로 바이트 오프셋을 추적하며 새로 추가된 내용만 전송합니다. 틱마다 가비지를 방지하기 위해 연결당 한 번 할당된 고정 32KB 읽기 버퍼를 재사용합니다. 매 틱마다 오프셋부터 EOF까지 루프 읽기하며, 32KB를 초과하는 델타는 여러 SSE 메시지로 분할되고 각각 동일한 `{"file": "...", "content": "..."}` 페이로드를 담습니다. 클라이언트는 도착하는 대로 내용을 추가하므로 분할은 투명합니다. `size <= lastOffset`(새 내용 없음)이면 파일을 열지 않고 틱을 건너뜁니다.

여러 역할이 동시에 로그를 쓸 때 — 병렬 딥 리뷰 sub-reviewer나 동시 reviewer + tester — 스트림은 가장 최근 수정된 로그 하나가 아닌 **모든** 현재 활성 로그를 테일합니다. `?file=` 쿼리 파라미터 없이는 최근 기간 내 mtime에 해당하는 모든 로그를 각각의 오프셋으로 추적하며, 메시지별 `file` 필드로 클라이언트가 내용을 맞는 패널로 라우팅할 수 있습니다. `?file=<name>`을 전달하면 스트림을 단일 로그에 고정합니다.

### 다중 프로젝트 라우팅

여러 프로젝트가 있을 때 엔드포인트 앞에 `/api/project/{project-id}/...` 및 `/sse/project/{project-id}/...`가 붙습니다. 단일 프로젝트 모드는 하위 호환성을 위해 접두사 없는 경로를 사용합니다.

#### 워크스페이스 해석

리프 라우트(`/api/tasks`, `/api/settings`, `/api/run`, `/api/batch/*`, `/sse/events/...` 등)는 `NewMux`(`internal/server/server.go`)에서 **한 번만** 정의됩니다. 고정된 워크스페이스를 바인딩하는 대신, `NewMux`는 `WorkspaceResolver`를 받습니다 — 들어오는 요청에서 대상 `*protocol.CachedWorkspace`, `*ProcessManager`, `*BatchManager`를 반환하는 함수(또는 오류). 각 데이터 백업 핸들러는 먼저 해석기를 호출하며, 이들이 필요 없는 라우트(`/api/user-config`, `/api/supported-runners`, `/api/locales`, 정적 에셋)는 건너뜁니다. 이를 통해 단일 및 다중 프로젝트 모드가 이전에 각각 가지고 있던 ~150줄의 중복 핸들러 등록이 제거됩니다.

두 개의 해석기가 두 모드를 지원합니다:

- **`singleResolver(ws, pm)`** — 단일 프로젝트 모드(`server.Start`). 하나의 워크스페이스를 클로저로 감싸고 항상 같은 `ws`/`pm`/`bm` 트리플을 반환합니다.
- **`multiResolver(reg)`** — 다중 프로젝트 모드(`NewMultiMux`). 해석은 세 단계 흐름입니다:
  1. **접두사 디스패치 (외부 mux).** `NewMultiMux`가 `/api/project/`와 `/sse/project/` 핸들러를 등록하여 `/api/project/{id}` (또는 `/sse/project/{id}`) 접두사를 제거하고, `getEntry(id)`로 항목을 조회하며(알 수 없는 id → **404**), `r.URL.Path`를 나머지 하위 경로로 재작성하고, 해석된 항목을 요청 `context`에 주입한 후 공유 내부 `NewMux` 핸들러로 전달합니다. 접두사 제거는 외부 mux에서 해야 합니다 — `http.ServeMux`는 핸들러를 실행하기 **전에** 선택하므로, 제거되지 않은 `/api/project/{id}/api/tasks`는 정적 `/` 라우트에만 매칭됩니다.
  2. **컨텍스트 읽기.** 내부 핸들러에서 `multiResolver`는 먼저 1단계에서 주입된 항목을 요청 컨텍스트에서 확인하고, 있으면 직접 반환합니다.
  3. **접두사 없는 호환.** 주입된 항목이 없으면(접두사 없는 경로) `reg.Count()`에 의존합니다: `0` → **400** `프로젝트 미로드`, `1` → 해당 단일 프로젝트, `>=2` → **400** `여러 프로젝트 로드됨 — /api/project/{id}… 사용 필요`.

`NewMultiMux` 자체는 글로벌 엔드포인트(`/api/projects`, `/api/projects/`, `/api/browse`)와 두 개의 접두사 디스패처, 그리고 공유 `inner := NewMux(multiResolver(reg))`로 전달하는 캐치올만 등록합니다. 프로젝트 추가 시 항목별 mux를 더 이상 구성하지 않습니다; `registryEntry`는 `id`/`ws`/`pm`/`bm`만 담습니다.

## 키보드 단축키

| 단축키 | 동작 |
|---|---|
| `Cmd+K` | 검색 |
| `Cmd+,` | 프로젝트 설정 (프로젝트 내) / 전역 설정 (홈에서) |
| `Cmd+Shift+,` | 전역 설정 |
| `Esc` | 현재 모달 닫기 |

## 프로세스 관리

대시보드는 러너 서브프로세스를 관리합니다:

- 프로젝트 설정의 `max_concurrent_runs` 준수
- stdout/stderr를 run-output/run-error 이벤트로 캡처
- 정상 종료: SIGTERM → 5초 → SIGKILL

러너 서브프로세스가 종료되면 서버가 기능을 비활성(`Active=false`, `StopReason=process-exit`)으로 표시합니다. 이는 경합에 대비하여 보호됩니다: 러너가 종료 직전에 자체 최종 `state.json`(예: `pending-review`)을 쓸 수 있습니다. 서버는 프로세스 종료 시간을 기록하고, 덮어쓰기 전에 상태를 다시 읽습니다 — `state.json`이 종료 시간 **이후**에 업데이트되었으면(`UpdatedAt >= endTime`), 러너의 최종 상태가 유지되고 비활성 쓰기가 건너뛰어집니다. 이를 통해 서버가 새로 쓴 단계를 되돌리거나 `StopReason`을 부실한 인메모리 스냅샷으로 덮어쓰는 것을 방지합니다.

## 공유 웹 프론트엔드

대시보드 UI(HTML/CSS/JS + 로케일 JSON)는 `dashboard/web/`에 단일 진실 소스로 존재하며, `dashboard/web/embed.go`(`web.Assets embed.FS`)를 통해 `4x` 바이너리에 임베드됩니다. Go 서버(`internal/server/server.go`, `internal/server/multi.go`)는 `web.Assets`에서 직접 정적 에셋과 로케일 파일을 서빙하므로, 동일한 프론트엔드가 모든 플랫폼 셸 — Go 서빙 웹 UI, macOS WKWebView, Tauri webview — 을 지원합니다. 동기화해야 할 플랫폼별 UI 복사본이 없습니다.

## 플랫폼

| 플랫폼 | 셸 | 패키징 |
|---|---|---|
| 웹 UI (내장) | Go 서버가 `web.Assets` 서빙 | `4x live` |
| macOS 네이티브 | Swift WKWebView, 번들된 `4x live` 서버 자동 실행 | 유니버설 `.dmg` (`make package-macos`) |
| Windows | Tauri v2 webview, `4x` 사이드카 | `.msi` (`dashboard/tauri`) |
| Linux | Tauri v2 webview, `4x` 사이드카 | `.AppImage` (`dashboard/tauri`) |

모든 데스크톱 셸은 내장된 `4x` 서버가 지원하는 `http://localhost:<port>`를 통해 동일한 `dashboard/web/` 프론트엔드를 로드합니다. `.github/workflows/desktop.yml`의 CI 매트릭스가 플랫폼별 `4x` 바이너리를 크로스 컴파일하고 `.dmg` / `.msi` / `.AppImage` 아티팩트를 생산합니다.
