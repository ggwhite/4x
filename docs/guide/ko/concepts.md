# 핵심 개념

## 네 가지 역할

| 역할 | 책임 | 입력 | 출력 | 제한 |
|---|---|---|---|---|
| **Designer** | 요구사항 분석, 스펙 작성, 인수 기준 및 테스트 전략 정의 | 기능 설명, 코드베이스 | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | 소스 코드 수정 불가 |
| **Coder** | 스펙에 명시된 대로 구현 | `task-brief.md`, 이전 테스트/리뷰 보고서 | 소스 코드, `coder-report.md` | 인수 기준 또는 테스트 스크립트 수정 불가 |
| **Reviewer** | 버그, 보안 문제, 스펙 위반 포착 | 디프, 스펙, 코더 보고서, 프로젝트 규칙 | `review-report.md` | 소스 코드 수정 불가 |
| **Tester** | 증거를 기반으로 인수 기준 검증 | 인수 기준, 코더 보고서, 테스트 전략 | 테스트 스크립트, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | 소스 코드 수정 불가 |

각 역할은 **격리**되어 있습니다 — Coder는 구현 중에 이전 리뷰 피드백을 보지 못합니다. Tester는 Coder가 아닌 Designer가 작성한 기준으로 검증합니다.

### 리뷰: 두 단계

1. **체크리스트 리뷰** (표준 모델) — 프로젝트 하드 규칙에 대한 검사: 보안, 동시성, 오류 처리, 스타일
2. **적대적 리뷰** (딥 모델) — "이 디프에 숨어 있는 최악의 버그는?" 심각도별로 발견 사항 평가.

### 에스컬레이션

Coder 또는 Tester는 다음과 같은 경우 Designer에게 에스컬레이션할 수 있습니다:

| 사유 | 의미 |
|---|---|
| `spec-mismatch` | DB/API가 스펙과 일치하지 않음 |
| `criteria-wrong` | 인수 기준이 올바르지 않음 |
| `blocker` | 누락된 의존성 또는 인프라 문제 |
| `scope-change` | 범위 밖의 리포지토리 수정 필요 |

에스컬레이션은 `escalation.json`에 기록됩니다. 루프가 자동으로 Designer에게 다시 라우팅합니다.

---

## 상태 머신

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     ├── amending ←──────────┘
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
| `testing` | `accepting`, `amending`, `designing` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| any | `blocked`, `needs-attention` |

### 라운드 카운터

- 라운드가 0일 때 `coding`에 진입하면 라운드를 1로 설정
- `amending`에 진입하면 라운드가 증가
- 라운드 >= maxRounds이거나 진행 없이 3회 이상 연속될 때 `ShouldStop` 발동

### 루프 내 단계별 결정

| 단계 | 조건 | 동작 |
|---|---|---|
| `designing` | `task-brief.md` 누락 | → `needs-attention` |
| `coding` / `amending` | `escalation.json`에 `spec-mismatch` 또는 `criteria-wrong` | → `designing` |
| `reviewing` | 판정 라인이 FAIL로 시작하거나 `[CRITICAL]` 포함 | → `amending` |
| `testing` | `verify.json` 미통과 또는 아티팩트 누락 | → `amending` |

---

## 파일 프로토콜

역할은 공유 컨텍스트 윈도우가 아닌 `.4x/` 디렉토리를 통해 통신합니다.

```
.4x/
├── settings.json                    # 프로젝트 설정
├── plugins/                         # 러너 지침 파일
├── batch-plan.json                  # 배치 실행 계획
├── batch-stop                       # 정상 종료 신호
├── features/
│   └── {id}.yaml                    # 기능 정의 (정식 소스)
└── {feature-id}/
    ├── state.json                   # 단계, 역할, 라운드, 활성, 러너, runners, 중지 사유
    ├── events.jsonl                 # 감사 추적
    ├── baseline.json                # 코딩 전 스냅샷 (HEAD, 브랜치, dirty 파일)
    ├── task-brief.md                # Designer → Coder: 스펙 + 아키텍처
    ├── acceptance-criteria.md       # Designer → Tester: 테스트 가능한 기준
    ├── test-strategy.yaml           # Designer → Tester: 테스트 접근법
    ├── final-report.md              # 루프 종료 요약
    ├── commit-plan.md               # 변경사항을 커밋으로 분할하는 방법
    ├── logs/
    │   └── round-{N}-{role}.log     # 라운드별/역할별 실행 로그
    └── rounds/round-{N}/
        ├── coder-report.md          # Coder의 작업 내용
        ├── review-report.md         # Reviewer 발견 사항 + 판정
        ├── test-report.md           # Tester 결과
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### 기능 YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status`는 빠른 목록 조회를 위해 `state.json`의 단계를 미러링합니다. `depends`는 이 기능을 실행하기 전에 완료되어야 하는 기능 ID를 나열합니다.

---

## 가드레일

CLI에서 시행되는 확정적 검사 — AI 판단에 의존하지 않습니다.

| 가드레일 | 역할 |
|---|---|
| **필수 파일** | 단계에 적합한 아티팩트가 존재하는지 확인 (예: designing 후 `task-brief.md`) |
| **베이스라인** | 코딩 전 상태를 캡처 (HEAD, 브랜치, dirty 파일); dirty 파일이 있으면 경고 |
| **범위** | `git diff --name-only HEAD` 최상위 디렉토리를 기능이 선언한 repos와 비교 |
| **의존성** | 의존 기능이 완료되지 않으면 `4x run` 차단 |
| **백로그 드리프트** | `.4x/features/*.yaml`과 외부 미러가 동기화되지 않으면 경고 |
| **Testing → Accepting 게이트** | `verify.json`(passed=true), `test-report.md`, `final-report.md`, `commit-plan.md` 필요 |

`4x check <feature-id>`로 수동 실행 가능합니다.

---

## Pending Review 게이트

루프는 `done`으로 **직접 이동하지 않습니다**. accepting 후 기능은 `pending-review`로 진입합니다 — 사람이 AI의 작업을 리뷰하기를 기다립니다.

```
... → accepting → pending-review → (사람이 리뷰) → 4x done F001
```

이를 통해 기능이 완료로 간주되기 전에 항상 사람이 승인하도록 보장합니다.
