# 핵심 개념

## 네 가지 역할

| 역할 | 책임 | 입력 | 출력 | 제한 |
|---|---|---|---|---|
| **Designer** | 요구사항 분석, 스펙 작성, 인수 기준 및 테스트 전략 정의 | 기능 설명, 코드베이스 | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | 소스 코드 수정 불가 |
| **Coder** | 스펙에 명시된 대로 구현 | `task-brief.md`, 이전 테스트/리뷰 보고서 | 소스 코드, `coder-report.md` | 인수 기준 또는 테스트 스크립트 수정 불가 |
| **Reviewer** | 버그, 보안 문제, 스펙 위반 포착 | 디프, 스펙, 코더 보고서, 프로젝트 규칙 | `review-report.md` | 소스 코드 수정 불가 |
| **Tester** | 증거를 기반으로 인수 기준 검증 | 인수 기준, 코더 보고서, 테스트 전략 | 테스트 스크립트, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | 소스 코드 수정 불가 |

각 역할은 **격리**되어 있습니다 — Coder는 구현 중에 이전 리뷰 피드백을 보지 못합니다. Tester는 Coder가 아닌 Designer가 작성한 기준으로 검증합니다.

### 추가 루프 역할

두 가지 추가 역할이 루프 후반부에서 동작합니다:

| 역할 | 단계 | 책임 |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | 적대적 리뷰 — 전체 디프에서 최악의 버그를 찾아냅니다 |
| **Acceptor** | `accepting` | 모든 라운드 산출물을 종합하여 `final-report.md`와 `commit-plan.md`를 작성하고 사람의 리뷰를 대기합니다 |

Acceptor는 자체 전용 모델 설정(`roles.acceptor.model`)을 사용하며 — Designer와 구분됩니다. 최종 요약을 작성하기 전에 모든 라운드 산출물을 읽습니다.

### 파이프라인 프로파일

**파이프라인 프로파일**은 주어진 기능에 대해 어떤 역할을 실행할지 선택하여, 단순한 작업에서는 전체 6역할 파이프라인 대신 일부 역할을 건너뛸 수 있게 합니다. 기본 제공 프로파일:

| 프로파일 | 역할 |
|---|---|
| `full` | designer, coder, reviewer, tester, deep-reviewer, acceptor |
| `normal` | coder, reviewer, tester, acceptor |
| `quick` | coder, reviewer |

`coder`는 항상 필수입니다. `profiles`가 설정되어 있으면 기능의 우선순위에 따라 자동 선택됩니다(최고 우선순위 → `full`, 그다음 `normal`, 그다음 `quick`). `--profile` 플래그로 선택을 재정의할 수 있습니다. 활성 프로파일에 포함되지 않은 역할은 건너뛰어집니다 — 루프는 러너를 호출하지 않고 동일한 유효 상태 전환 경로를 따라 진행합니다. `profiles`, `parallel_review_test`, `coder_model` 설정에 대해서는 [설정](configuration.md)을 참조하세요.

### 리뷰: 두 단계

1. **체크리스트 리뷰** (표준 모델) — 프로젝트 하드 규칙에 대한 검사: 보안, 동시성, 오류 처리, 스타일
2. **적대적 리뷰** (딥 모델) — "이 디프에 숨어 있는 최악의 버그는?" 심각도별로 발견 사항 평가.

### 딥 리뷰 자가 치유

Deep Reviewer가 차단 이슈를 발견하면, `deep-reviewing` 단계에서 `amending → reviewing → testing`의 전체 경로를 거치지 않고 **즉시 수정**합니다. Reviewer와 Tester가 딥 리뷰 전에 이미 통과했으므로, 비용이 많이 드는 전체 체인(특히 딥 모델)을 재실행하는 것은 낭비입니다.

같은 단계 내에서 루프는 두 개의 한정된 하위 역할을 생성하고, 보고서가 통과하거나 상한에 도달할 때까지 반복합니다:

| 하위 역할 | 모델 | 입력 | 출력 | 범위 |
|---|---|---|---|---|
| **mini-coder** | coder 모델 | `deep-review-report.md`의 `## Issues` 섹션만 (`task-brief.md` 아님) | 소스 코드, `coder-report.md` | 딥 리뷰어가 지목한 이슈만 |
| **re-verifier** | reviewer 모델 | 이전 이슈 + 해당 반복의 mini-coder 디프 | `deep-reverify-{n}.md`, `deep-review-report.md`의 `## Verdict` 업데이트 | 이전 이슈가 수정되었는지 확인하고 새 디프에 버그가 없는지 검증 |

이 단계는 전체적으로 `deep-reviewing`을 유지합니다 — 하위 역할은 상태 머신 단계가 아닙니다. re-verifier가 깨끗한 PASS를 확인하면 루프는 `accepting`으로 진행합니다. 루프는 최대 `roles.deep-reviewer.max_fix_rounds` 반복(기본값 2)만 실행하며, mini-coder가 기능 범위 밖의 파일을 수정하거나 상한에 도달했는데 여전히 실패하면 FAIL 보고서를 보존한 채 `needs-attention`으로 에스컬레이션합니다.

### 병렬 딥 리뷰

딥 리뷰는 11개의 고유한 관점(정확성, 품질, 관례, 이력, 피드백 등)을 다룹니다. `roles.deep-reviewer.parallel_reviewers`가 1보다 크면, 하나의 에이전트에게 11개 전부를 맡기는 대신 여러 집중 하위 리뷰어에게 관점을 분산합니다. 이는 `/code-review`가 리뷰를 차원별로 분할하는 방식과 같으며, 각 에이전트의 컨텍스트 부담과 주의력 분산을 줄입니다.

팬아웃은 전적으로 4x CLI가 주도하며 — LLM 자체의 하위 에이전트나 도구 기능에 의존하지 않습니다. `deep-reviewing` 단계는 단일 단계로 유지됩니다:

| 하위 역할 | 모델 | 입력 | 출력 |
|---|---|---|---|
| **sub-reviewer** (xN) | 딥 모델 | 디프 + 할당된 관점 하위 집합 | `deep-review-partial-{i}.md` |
| **synthesizer** | 딥 모델 | 모든 부분 보고서의 전체 내용 | `deep-review-report.md` |

관점은 균등하게 겹침 없이 분할됩니다: 기본 `parallel_reviewers: 3`에서 그룹은 `[1-4]`, `[5-8]`, `[9-11]`(정확성 / 품질+관례 / 이력+피드백)입니다. `roles.deep-reviewer.angles_per_reviewer`를 설정하면 그룹 크기를 명시적으로 지정할 수 있으며, 비워두면 자동으로 `ceil(11/N)` 균형이 적용됩니다. N개의 sub-reviewer가 병렬로 실행된 후, 단일 synthesizer가 중복을 제거하고 충돌을 중재하며 신뢰도 점수를 통합하여 자가 치유 루프와 `parseReviewVerdict`가 이미 사용하는 것과 동일한 `deep-review-report.md` 형식으로 통합합니다 — 따라서 하위 단계는 모두 변경 없이 동작합니다.

`parallel_reviewers`가 설정되지 않았거나 `<= 1`이면, 루프는 원래의 단일 에이전트 흐름으로 폴백합니다: 한 명의 딥 리뷰어가 11개 관점을 모두 렌더링하고 `deep-review-report.md`를 직접 작성하며, 부분 보고서나 synthesizer는 없습니다.

### 자동 발견 기능

딥 리뷰어는 실제 문제이지만 **현재 기능의 범위 밖**인 이슈를 종종 발견합니다 — 잠재적 버그, 기술 부채, 누락된 기능 등. 기록할 곳이 없으면 이런 메모는 보고서에 묻히게 됩니다. `auto_discover_features`가 활성화되면 실행 루프가 자동으로 이를 캡처합니다.

딥 리뷰어는 범위 밖 후보를 `deep-review-report.md`의 `## Discovered Issues` 섹션에 `[NEW-FEATURE] <제목>` 블록(짧은 설명 포함)으로 작성합니다. **최종 딥 리뷰 PASS** 후(`accepting`에 도달하는 두 가지 경로만 — 첫 번째 PASS와 자가 치유 re-verifier의 PASS 전환), 루프는 이 블록을 파싱하고 CLI 계층에서 완전히(LLM 호출 없이) 처리합니다:

- 각 후보를 기존 기능 및 이미 유지된 후보와 Jaccard 토큰 유사도 검사로 **중복 제거**합니다.
- `max_discovered_features`(기본값 `3`)까지 **수량 제한**하며, 나머지는 제한됨으로 기록됩니다.
- 유지된 후보를 새 기능 YAML(상태 `not-started`, `4x new`와 동일한 번호 체계)로 **생성**하고, 생성 시마다 `feature-discovered` 이벤트를 추가합니다.
- 결과(생성됨 / 중복으로 건너뜀 / 제한됨)를 `.4x/{feature-id}/discovered-features.md`에 **요약**합니다.

이 단계는 최선의 노력으로 수행됩니다: 오류가 발생해도 `accepting`으로의 전환을 차단하지 않습니다. 최종 딥 리뷰 PASS에서만 실행됩니다 — 중간 라운드와 FAIL/`needs-attention` 경로에서는 실행되지 않습니다. 설정에 대해서는 [설정 → 자동 발견 기능](configuration.md#auto-discover-features)을 참조하세요.

### 에스컬레이션

Coder 또는 Tester는 다음과 같은 경우 에스컬레이션할 수 있습니다:

| 사유 | 의미 | 라우팅 대상 |
|---|---|---|
| `spec-mismatch` | DB/API가 스펙과 일치하지 않음 | Designer |
| `criteria-wrong` | 인수 기준이 올바르지 않음 | Designer |
| `blocker` | 누락된 의존성 또는 인프라 문제 | `needs-attention` (사람 개입) |
| `scope-change` | 범위 밖의 리포지토리 수정 필요 | Designer |

에스컬레이션은 `escalation.json`에 기록됩니다. 루프가 자동으로 `spec-mismatch`, `criteria-wrong`, `scope-change`를 Designer에게 다시 라우팅합니다. `blocker` 에스컬레이션은 사람 개입을 위해 `needs-attention`으로 이동합니다.

---

## 상태 머신

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
                     ↑      ↓
                     └──────┘
```

### 모든 유효한 전환

| From | To |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`, `designing` |
| `reviewing` | `testing`, `amending` |
| `amending` | `reviewing`, `designing` |
| `testing` | `deep-reviewing`, `amending`, `designing` |
| `deep-reviewing` | `accepting`, `amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| any | `blocked`, `needs-attention`, `done`, `abandoned` |

### 라운드 카운터

- 라운드가 0일 때 `coding`에 진입하면 라운드를 1로 설정
- `amending`에 진입하면 라운드가 증가
- 라운드 >= maxRounds이거나 진행 없이 3회 이상 연속될 때 `ShouldStop` 발동

### 루프 내 단계별 결정

| 단계 | 조건 | 동작 |
|---|---|---|
| `designing` | `task-brief.md` 또는 `acceptance-criteria.md` 누락 | → `needs-attention` |
| `coding` / `amending` | `escalation.json`에 `spec-mismatch`, `criteria-wrong`, 또는 `scope-change` | → `designing` |
| `reviewing` | 리뷰 미통과 (명시적 `PASS` 또는 `CONDITIONAL PASS` 판정이 필요하며, 보고서에 `[CRITICAL]`/`[WARNING]` 이슈가 없어야 함) | → `amending` |
| `testing` | `verify.json` 미통과 또는 아티팩트 누락 | → `amending` |
| `deep-reviewing` | 딥 리뷰 FAIL | 즉시 자가 치유 (mini-coder + re-verifier), 최대 `max_fix_rounds`회; PASS → `accepting`, 아니면 → `needs-attention` |
| any (비 designer) | 가드 검사에서 범위 위반, 베이스라인 변동 또는 필수 파일 누락 발견 | → `needs-attention` |

---

## 파일 프로토콜

역할은 공유 컨텍스트 윈도우가 아닌 `.4x/` 디렉토리를 통해 통신합니다.

```
.4x/
├── settings.json                    # 프로젝트 설정
├── plugins/                         # 러너 지침 파일
├── batch-plan.json                  # 배치 실행 계획
├── batch-stop                       # 정상 종료 신호
├── batch-pid                        # 실행 중인 배치 서브프로세스의 PID (서버 고아 프로세스 인수)
├── batch-conflict.json              # 배치 자동 병합 충돌 신호 (일시정지)
├── batch-report.json                # 마지막 배치 실행 보고서 (통계 + 기능별 결과)
├── features/
│   └── {id}.yaml                    # 기능 정의 (정식 소스)
└── {feature-id}/
    ├── state.json                   # 단계, 역할, 라운드, 활성, 러너, runners, 중지 사유, 프로파일
    ├── events.jsonl                 # 감사 추적
    ├── baseline.json                # 코딩 전 스냅샷 (HEAD, 브랜치, dirty 파일)
    ├── task-brief.md                # Designer → Coder: 스펙 + 아키텍처
    ├── acceptance-criteria.md       # Designer → Tester: 테스트 가능한 기준
    ├── test-strategy.yaml           # Designer → Tester: 테스트 접근법
    ├── final-report.md              # 루프 종료 요약
    ├── commit-plan.md               # 변경사항을 커밋으로 분할하는 방법
    ├── logs/
    │   ├── round-{N}-{role}.log              # 라운드별/역할별 실행 로그
    │   ├── round-{N}-deep-reviewer-{i}.log   # 병렬 sub-reviewer별 (팬아웃 시)
    │   └── round-{N}-synthesizer.log         # 부분 보고서를 병합하는 synthesizer
    └── rounds/round-{N}/
        ├── coder-report.md            # Coder의 작업 내용
        ├── review-report.md           # Reviewer 발견 사항 + 판정
        ├── test-report.md             # Tester 결과
        ├── deep-review-partial-{i}.md # 병렬 sub-reviewer 하나의 발견 사항 (팬아웃 시)
        ├── deep-review-report.md      # 병합된 딥 리뷰 (synthesizer 출력 또는 단일 에이전트)
        ├── verify.json                # {passed, round, role, commands[]}
        └── escalation.json            # {needed, reason, detail}
```

### 배치 신호 파일

두 개의 최상위 신호 파일이 실행 중인 배치와 외부 관찰자(CLI 및 대시보드)를 조율합니다:

- **`batch-stop`** — 빈 마커 파일. `4x batch run`이 기능 사이에서 이 파일을 폴링하고 존재하면 정상 종료합니다([배치 모드](batch.md) 참조).
- **`batch-conflict.json`** — 배치 자동 병합이 병합 충돌에 부딪혀 일시정지할 때 기록됩니다. git을 다시 실행하지 않고도 대시보드가 충돌을 렌더링할 수 있도록 충분한 정보를 담고 있습니다:

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  `conflictRepo`는 모노레포 모드에서는 비어 있습니다. 이 파일은 각 배치 실행 시작 시와 사용자가 일시정지된 배치를 계속할 때 지워집니다.

- **`batch-report.json`** — 배치 실행이 종료될 때(정상, 중지, 인터럽트 또는 크래시) 기록됩니다. 위의 두 신호 파일과 달리 "마지막 배치 보고서"로 실행 간에 유지되며, 배치가 활성 상태가 아닐 때 대시보드에 표시됩니다. `outcome`, 전체 카운트(`total` / `completed` / `failed` / `remaining`), 러너, 총 소요 시간, 기능별 분석(최종 상태, 라운드, 중지 사유)을 기록하며, `crashed` 결과에는 `panicMessage`도 포함됩니다. 원자적으로 기록(임시 파일 + rename)되므로 대시보드가 반쯤 쓰인 보고서를 읽는 일은 없습니다.

### 원자적 상태 쓰기

`state.json`은 여러 주체 — 실행 루프, 대시보드 서버, 백그라운드 리콘실러 — 가 동시에 읽고 씁니다. 읽는 쪽이 절반만 쓰인 파일을 보지 않도록, `WriteState`는 절대 직접 덮어쓰지 않습니다. 상태를 마셜링하고, **같은 디렉토리**에 임시 파일(`.state-*.json`)을 쓴 다음(같은 파일시스템이므로 rename이 원자적임), `os.Rename`으로 `state.json` 위에 덮어씁니다. 따라서 읽는 쪽은 항상 완전한 이전 파일이나 완전한 새 파일만 보며 — 절대 부분적인 파일은 보지 않습니다. 실패 시 임시 파일은 삭제되어 `.state-*.json` 잔해가 쌓이지 않습니다. 파일 잠금은 사용하지 않으며, 정확성은 원자적 rename과 `UpdatedAt` 비교에 의해 보장됩니다.

### 워크스페이스 읽기 캐시 (대시보드 서버)

CLI는 단명 프로세스입니다: 각 명령어가 필요한 `.4x/` 파일을 한 번 읽고 종료하므로 항상 일반 `*protocol.Workspace`를 사용합니다. 대시보드 서버(`4x live`)는 반대로 — 장기 실행되며 모든 API 요청이 같은 파일을 다시 읽습니다. 다중 프로젝트 x 다중 기능 워크스페이스(예: 5 프로젝트 x 50 기능)에서는 단일 요청이 수백 번의 YAML/JSON 파싱을 유발할 수 있습니다.

이를 방지하기 위해 서버는 각 워크스페이스를 `*protocol.CachedWorkspace`(`internal/protocol/cached.go`)로 감쌉니다. 이는 `WorkspaceReader` 인터페이스(`internal/protocol/reader.go`)가 선언한 읽기 전용 연산에 대한 mtime 기반 인메모리 캐시입니다:

- **`ReadConfig`** — `settings.json`을 캐시하며, `os.Stat`으로 파일 mtime을 비교하여 변경 시에만 다시 파싱합니다.
- **`ListFeatures`** — 전체 기능 목록을 캐시하며, `os.ReadDir`로 `.yaml` 파일 세트와 각 파일의 mtime을 비교하여 파일이 추가, 삭제 또는 수정된 경우에만 다시 파싱합니다. 호출자가 자유롭게 변경할 수 있도록 복사본을 반환합니다. 느슨한 검증을 사용합니다: 형식에 문제가 있는 기능(예: 잘못된 subtask status)도 건너뛰지 않고 `Warnings`를 포함하여 목록에 표시합니다.
- **`LoadFeature`** — YAML의 mtime을 키로 각 기능을 ID별로 캐시합니다. 엄격한 검증을 사용합니다 — 형식 문제가 있으면 error를 반환합니다.
- **`ReadState`** — 의도적으로 **캐시하지 않습니다** (자주 변경, 작은 파일, 빠른 파싱); 내장된 `*Workspace`로 직접 전달합니다.

무효화는 암시적입니다: 쓰기 메서드(`SaveFeature`, `WriteState` 등)가 캐시에 알릴 필요 없이 다음 읽기에서 새 mtime을 감지합니다. 캐시는 선택적입니다 — 서버만 `CachedWorkspace`를 구성하며, CLI는 동일한 동작으로 `*Workspace`를 계속 사용합니다. Go 임베딩에는 가상 디스패치가 없으므로, 내부 `*Workspace` 메서드 호출(예: `CompareBacklogMirror`가 `w.ListFeatures()`를 호출)은 여전히 캐시되지 않은 원본을 실행합니다; 이 경로는 서버 핫패스가 아니므로 허용됩니다.

### 기능 YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # 숫자: 0-1 = full 프로파일, 2 = normal, 3+ = quick (nil/미설정 시 생략)
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # 설계 스펙의 선택적 명시 경로 (docs/design/ 조회를 재정의)
plan: ""     # 구현 계획의 선택적 명시 경로
hooks: {}    # 선택적 단계 훅 (settings.json과 동일한 형식)
```

`status`는 빠른 목록 조회를 위해 `state.json`의 단계를 미러링합니다. 유효한 값: `not-started`, `in-progress`, `ready-for-review`, `needs-attention`, `blocked`, `done`, `abandoned`. `abandoned` 기능은 완료로 취급(의존성을 차단하지 않음)하지만 대시보드에서 취소선으로 표시됩니다. `depends`는 이 기능을 실행하기 전에 완료(또는 abandoned)되어야 하는 기능 ID를 나열합니다. `repos`는 이 기능이 다루는 리포지토리 이름(`workspace.repos` 기준)을 나열하며, 비어 있으면 모든 리포지토리가 범위에 포함됩니다.

#### 설계 문서 해석

대시보드 개요와 `4x prompt` 계획 문서 주입은 하나의 공유 해석기(`protocol.ResolveDesignDoc`)를 통해 기능의 spec/plan을 찾으므로, 둘 다 항상 같은 문서를 봅니다. 문서 유형(`spec`/`plan`)별 해석 순서:

1. 기능 YAML의 `spec`/`plan` 필드를 경로로 읽습니다(상대 경로는 워크스페이스 루트 기준으로 해석). 비어 있지 않을 때 적용.
2. `docs/design/{feature.ID}-{type}.md`.
3. `docs/design/{slug}-{type}.md` — `slug`는 ID에서 `FNNN-` 접두사를 제거한 것(ID와 다를 때만 시도).

첫 번째로 존재하는 파일이 적용됩니다. 어느 것도 일치하지 않으면 문서가 없는 것으로 처리됩니다.

### 기능 생성

`Feature`/`Subtask`/`Status` 타입과 생성 로직은 독립적인 `internal/feature` 패키지에 있습니다(ID 생성, 백로그 드리프트, 스크린샷 헬퍼도 여기로 이동). `protocol.Workspace`와 `protocol.CachedWorkspace`는 `feature.Store` 인터페이스를 만족하며, `feature`는 `protocol`을 임포트하지 않습니다(단방향 의존성, `Store`를 통한 디커플링). CLI(`4x new`)와 대시보드(`POST /api/new`) 모두 단일 `feature.Create(store, opts)` 진입점을 통해 기능을 생성하므로, 번호 매기기, ID 잘림, 기본 필드가 진입점에 관계없이 동일하게 동작합니다.

### 워크스페이스 설정 (멀티 리포)

기본적으로 4x는 모노레포 모드로 동작합니다. 여러 리포지토리에 걸쳐 작업하려면 `.4x/settings.json`에 선언합니다:

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

각 항목은 리포지토리 이름을 경로(워크스페이스 루트 기준 상대 경로)와 선택적 `hub` 플래그에 매핑합니다. Hub 리포지토리는 여러 기능이 접근할 수 있는 공유 인프라로, `4x batch plan`의 범위 클러스터링에서 제외됩니다.

모노레포 모드(`workspace.repos` 없음)에서는 모든 범위 검사와 git 작업이 단일 리포지토리 루트를 사용합니다.

---

## 가드레일

CLI에서 시행되는 확정적 검사 — AI 판단에 의존하지 않습니다.

| 가드레일 | 역할 |
|---|---|
| **필수 파일** | 단계에 적합한 아티팩트가 존재하는지 확인 (예: designing 후 `task-brief.md`) |
| **베이스라인** | 코딩 전 상태를 캡처 (HEAD, 브랜치, dirty 파일); dirty 파일이 있으면 경고 |
| **범위** | 모노레포 모드: `git diff --name-only HEAD` 최상위 디렉토리를 기능이 선언한 repos와 비교. 멀티 리포 모드: 모든 워크스페이스 리포지토리에서 `gitops.Ops.DetectChangedRepos()` 사용 |
| **의존성** | 의존 기능이 완료되지 않으면 `4x run` 차단 |
| **백로그 드리프트** | `.4x/features/*.yaml`과 외부 미러가 동기화되지 않으면 경고 |
| **Testing → Accepting 게이트** | `verify.json`(passed=true), `test-report.md`, `final-report.md`, `commit-plan.md` 필요 |

`4x check <feature-id>`로 수동 실행 가능합니다.

---

## 단계 훅

단계 훅을 사용하면 단계 전환 전후에 셸 명령어를 자동 실행할 수 있습니다 — Docker 컨테이너 시작, 테스트 데이터베이스 시딩, 테스팅 후 정리 등에 유용합니다. 훅은 AI 역할이 아닌 CLI가 실행합니다.

### 설정

훅은 `settings.json`의 `hooks` 키 아래에 선언합니다. 키 형식은 `pre_{phase}` 또는 `post_{phase}`입니다:

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

각 항목은 두 개의 필드를 가진 `HookEntry`입니다:

| 필드 | 타입 | 설명 |
|---|---|---|
| `run` | string | `sh -c`로 실행되는 셸 명령어 |
| `on_fail` | string | `"block"` (기본값) 또는 `"warn"` (대소문자 무시) |

기능 YAML 파일도 동일한 형식의 `hooks` 필드를 선언할 수 있습니다. 기능이 글로벌 설정과 같은 키에 대해 훅을 정의하면, 기능의 정의가 글로벌을 **완전히 대체**합니다(키 내 병합 없음).

### 실행 순서

```
pre_{target_phase} 훅 (배열 순서대로)
  ↓ on_fail=block 훅이 실패하면 → needs-attention으로 전환, 중단
state.Transition()
  ↓
전환 이벤트 기록
  ↓
post_{target_phase} 훅 (배열 순서대로)
  ↓ on_fail=block 훅이 실패하면 → needs-attention으로 전환 (롤백 없음)
```

### 실패 동작

| `on_fail` | 훅 실패 | 효과 |
|---|---|---|
| `block` (기본값) | pre 훅 | 기능이 `needs-attention`으로 이동; 단계 전환 중단 |
| `block` (기본값) | post 훅 | 단계가 이미 변경됨; 기능이 `needs-attention`으로 이동 |
| `warn` | 둘 다 | 결과가 로그에 기록됨; 실행 계속 |

### 로깅

각 훅 실행은 `events.jsonl`에 `type: "hook"` 이벤트를 추가합니다:

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

전체 stdout/stderr 출력은 `.4x/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`에 기록됩니다.

### 훅 병합 (`MergeHooks`)

글로벌과 기능 훅은 `MergeHooks`로 병합됩니다: 모든 글로벌 키를 복사한 후, 기능 키가 동일한 이름의 글로벌 키를 완전히 재정의합니다. 글로벌에만 있는 키는 보존됩니다. 둘 다 nil이면 nil을 반환합니다.

---

## 헬스 체크

Tester 역할이 시작되기 전에, CLI가 자동으로 환경이 정상인지 확인할 수 있습니다 — 빌드 통과 여부, 서비스 상태, 엔드포인트 응답 여부 등. 여기서 잡힌 환경 문제는 테스트 사이클의 낭비를 방지합니다. 헬스 체크는 AI 역할이 아닌 CLI가 실행하며, `testing` 단계 진입 시, `pre_testing` 훅 이후 Tester 러너 생성 전에만 실행됩니다.

### 설정

헬스 체크는 세 개의 필드를 가집니다(`internal/protocol/types.go`의 `HealthCheck`):

| 필드 | 타입 | 설명 |
|---|---|---|
| `commands` | `[]string` | 순서대로 실행되는 검사 명령어; 하나라도 실패하면 실행 중단 |
| `recovery` | `[]string` | 선택 사항. 검사 실패 시 환경을 복구하기 위해 순서대로 실행 |
| `timeout` | `int` | 명령어당 타임아웃(초); `<= 0`이면 기본값 `30` 적용 |

`settings.json`에서 글로벌로 선언할 수 있습니다(JSON, yaml 태그 없음):

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

...또는 기능별로 `test-strategy.yaml`에서 선언(`Workspace.ReadTestStrategy`를 통해 읽기):

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**병합:** `ResolveHealthCheck`는 필드 수준 병합이 아닌 전체 그룹 재정의를 수행합니다. `test-strategy.yaml`에 `health_check`가 정의되어 있으면 글로벌을 완전히 대체하고, 그렇지 않으면 글로벌 설정이 사용됩니다. 둘 다 설정되지 않으면 헬스 체크를 건너뛰고 Tester가 즉시 시작됩니다.

### 실행 흐름

```
testing 단계 진입 (pre_testing 훅이 이미 실행됨)
  ↓
명령어를 순서대로 실행 (각각 자체 타임아웃 적용)
  ├─ 모두 통과 → Tester 시작
  └─ 하나라도 실패 →
      ├─ recovery 없음 → needs-attention으로 에스컬레이션
      └─ recovery 있음 → recovery 명령어를 순서대로 실행
          ├─ recovery 실패 → needs-attention으로 에스컬레이션
          └─ recovery 통과 → 모든 명령어를 한 번 더 실행
              ├─ 통과 → Tester 시작
              └─ 여전히 실패 → needs-attention으로 에스컬레이션
```

Recovery는 최대 한 번만 트리거됩니다 — 재시도 루프나 백오프는 없습니다.

### 실패 동작

최종 실패 시 루프는 `type: "health-check-failed"` 이벤트(역할 `tester`, 실패 명령어와 오류를 `detail`에 포함)를 기록하고, 기능을 `needs-attention`으로 전환하며, `StopReason`을 `health-check-failed`로 설정하고 루프를 중단합니다. 각 명령어는 `sh -c`로 명령어당 타임아웃 하에 실행됩니다; 타임아웃은 실패로 간주되며 출력은 디버깅을 위해 stderr에 기록됩니다.

---

## 테스트 프로파일

**테스트 프로파일**은 재사용 가능한 테스트 방법론 블록으로, Designer가 기능에 태그하면 Tester의 프롬프트에 해당 지침이 자동 주입됩니다 — 기능 유형에 관계없이 모든 기능이 공유하는 거대한 `roles.tester.instructions` 목록을 `settings.json`에서 수작업으로 유지하는 대신 사용합니다.

> **[파이프라인 프로파일](#파이프라인-프로파일)**(`Config.Profiles`)과 혼동하지 마세요. 파이프라인 프로파일은 *어떤 역할이 실행되는지* 선택합니다. 테스트 프로파일(`Config.TestProfiles`)은 Tester 프롬프트에 *테스트 방법론 내용*만 주입합니다.

### 프로파일 선언

Designer가 `test-strategy.yaml`에 프로파일을 나열합니다(`internal/protocol/types.go`의 `TestStrategy.Profiles`):

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles`는 `omitempty`입니다 — 이 필드가 없는 `test-strategy.yaml`은 이전과 정확히 동일하게 동작합니다(주입 없음).

### 기본 제공 프로파일

네 개의 프로파일이 바이너리에 내장되어 있습니다(`templates/profiles/*.md`, `templates.ProfilesFS`를 통해 노출):

| 프로파일 | 방법론 |
|---|---|
| `unit` | Go `go test`, `t.TempDir()` 격리, 테이블 드리븐, 오류 케이스, AC별 verify.json |
| `web` | `4x live` 대시보드에 대한 Playwright; headless, 격리된 워크스페이스 + 랜덤 포트, 증거로 스크린샷, 사용자의 실행 중인 서버와 간섭 없음 |
| `api` | HTTP 엔드포인트 테스트 — 상태 코드, 응답 본문, 엣지 케이스, 인증 |
| `e2e` | 엔드투엔드 다중 서비스 플로우, DB 상태와 교차 서비스 일관성 |

### settings.json에서 재정의

프로젝트는 `Config.TestProfiles`(`test_profiles`)를 통해 프로파일 이름을 키로 하여 어떤 프로파일이든 대체하거나 확장할 수 있습니다(`TestProfileOverride`):

```json
{
  "test_profiles": {
    "web": { "content": "Playwright 대신 Cypress로 테스트..." },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` — 인라인 대체 텍스트
- `include` — 내용이 사용될 파일 경로(워크스페이스 루트 기준 상대 경로)

**해석 순서** (프로파일 이름별): `test_profiles[name].content` → `test_profiles[name].include` → 기본 제공 `profiles/{name}.md`. 재정의는 필드 수준 병합이 아닌 전체 대체입니다. 알 수 없는 이름(재정의도 기본 제공도 없음)은 stderr에 경고를 출력하고 건너뜁니다.

Tester 프롬프트는 해석된 각 프로파일을 `== Test Profile: {name} ==` 블록으로 렌더링합니다. 로딩은 `loadProfiles` / `resolveProfileContent`(`cmd/4x/prompt.go`)에서 구현됩니다.

---

## Pending Review 게이트

루프는 `done`으로 **직접 이동하지 않습니다**. accepting 후 기능은 `pending-review`로 진입합니다 — 사람이 AI의 작업을 리뷰하기를 기다립니다.

```
... → accepting → pending-review → (사람이 리뷰) → 4x done F001
```

이를 통해 기능이 완료로 간주되기 전에 항상 사람이 승인하도록 보장합니다.
