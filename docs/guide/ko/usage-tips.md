# 사용 팁 & 모범 사례

## 토큰 사용량 안내

4x는 **단일 에이전트보다 상당히 많은** 토큰을 소비합니다. 각 기능은 최소 4개의 역할(Designer → Coder → Reviewer → Tester)을 거치며, 각 역할은 독립적인 LLM 호출입니다. Review 또는 Test 실패로 재실행이 발생하면 토큰이 다시 두 배가 됩니다.

기능당 토큰 사용량 대략적 추정:

| 시나리오 | 약 LLM 호출 횟수 | 설명 |
|---|---|---|
| 한 번에 통과 (최적의 경우) | 5회 | Designer + Coder + Reviewer(2 패스) + Tester |
| Review 1회 반려 | 8회 | Coder + Reviewer + Tester 1라운드 추가 |
| 5라운드 전체 실행 | ~20회 | 매 라운드마다 Coder + Reviewer + Tester |

**토큰 절약 조언:**
- 간단한 작업은 `--max-rounds`를 낮추세요 (`--max-rounds 2`)
- 간단한 작업은 모든 역할에 sonnet 등급 모델을 사용하세요 (5-10배 저렴)
- `--dry-run`으로 먼저 프롬프트 품질을 확인하여 낭비를 방지하세요
- 기능 설명을 명확히 작성하여 에스컬레이션과 재실행을 줄이세요
- 연속 3라운드 진행 없으면 루프가 자동 중지되어 max-rounds까지 헛되이 소비하지 않습니다

---

## AI 에이전트와 함께하는 실제 워크플로우

이것이 저자가 실제로 4x를 날마다 사용하는 방식입니다 — 순수 CLI 명령어가 아닌, 처음부터 끝까지 같은 대화 안에서 AI 지원 루프를 사용합니다.

### 1. 기능 생성

AI 에이전트에게 기능을 만들어달라고 요청합니다:

```
> 4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

### 2. 브레인스토밍 — 스펙 & 계획

루프를 실행하기 전에 에이전트에게 설계를 브레인스토밍해달라고 요청합니다:

```
> brainstorm F001
```

에이전트는 브레인스토밍 기술을 사용하여 요구사항, 트레이드오프, 엣지 케이스를 함께 탐색합니다. 정렬이 완료되면 두 가지 결과물이 생성됩니다:

- `docs/design/F001-add-redis-cache-for-or-spec.md` — 설계 스펙
- `docs/design/F001-add-redis-cache-for-or-plan.md` — 구현 계획

이 파일들은 `CLAUDE.md`의 **Docs Routing** 아래에 선언된 명명 규칙을 따릅니다: `docs/design/{feature-id}-spec.md`와 `docs/design/{feature-id}-plan.md`.

스펙은 Designer의 참고 입력이 됩니다 — 잘 브레인스토밍된 스펙은 Designer가 더 나은 task brief를 작성하게 하며, 이는 리뷰 거부와 재시도 라운드를 줄입니다.

### 3. 루프 실행

```bash
4x run F001 --runner claude
```

다른 터미널에서 대시보드를 열어 진행 상황을 봅니다:

```bash
4x live -w
```

### 4. AI 코드 리뷰

루프가 완료되면(`pending-review`), AI 에이전트에게 diff를 리뷰해달라고 요청합니다:

```
> help me review the diff on branch 4x/F001-add-redis-cache-for-or
```

에이전트가 `final-report.md`를 읽고 브랜치를 main과 diff하여 이슈를 지적합니다. 필요한 것을 수정합니다 — 직접 또는 에이전트에게 요청하여.

### 5. 머지 & 정리

만족스러우면 에이전트에게 머지하고 정리해달라고 요청합니다:

```
> merge it and clean up the worktree
```

에이전트가 실행합니다:
```bash
4x done F001
```

`4x done`이 자동으로 브랜치를 병합하고 worktree를 제거하며 브랜치를 삭제합니다. 병합 충돌이 발생하면 수동으로 해결한 후 `4x merge F001`을 실행하라는 안내가 표시됩니다.

### 6. 대시보드에서 완료 표시

대시보드(`4x live -w`)를 열고 기능 카드의 **Mark Done**을 클릭합니다. 이것은 의도적으로 사람이 하는 작업입니다 — AI 루프는 절대 기능을 자동 완료하지 않습니다.

### 이것이 효과적인 이유

- **코딩 전 브레인스토밍** — 스펙이 전체 루프의 기반이 됩니다; 모호함이 구현 중이 아닌 사전에 해소됩니다
- **하나의 대화를 유지** — 터미널과 도구 사이를 오가는 컨텍스트 전환이 없습니다
- **AI 에이전트가 이미 완전한 컨텍스트를 가짐** — 브레인스토밍과 기능 실행을 통해 정보에 기반한 리뷰가 가능합니다
- **완료 표시는 수동** — 당신이 최종 결재자이지, AI가 아닙니다

### 4x란 무엇인가 (그리고 아닌 것)

4x는 **워크플로우 오케스트레이터**입니다 — Designer, Coder, Reviewer, Tester 역할을 순서대로 실행하고 그 사이의 상태 머신을 관리합니다. 당신의 판단을 대체하지 않습니다.

실제로 루프는 해피 패스를 잘 처리합니다: 명확한 스펙을 가진 단순한 기능은 보통 1-2라운드에 통과합니다. 그러나 실제 개발은 복잡합니다:

- **Coder가 스펙을 오해할 수 있습니다** — Reviewer가 잡아내지만, 다음 라운드의 수정이 여전히 요점을 놓칠 수 있습니다. 2-3번 실패한 라운드 후에는 직접 개입하거나 AI 에이전트에게 특정 이슈를 직접 수정해달라고 하는 것이 더 빠릅니다.
- **테스트 실패가 환경 특이적일 수 있습니다** — Tester가 스펙을 기반으로 테스트를 작성하지만, 프로젝트에 특이사항(커스텀 테스트 설정, 불안정한 CI, 레거시 제약)이 있으면 AI가 진단할 수 없는 이유로 테스트가 실패할 수 있습니다.
- **엣지 케이스가 루프 후에 나타납니다** — 4x는 스펙이 설명하는 것을 다룹니다. 비즈니스 로직 엣지 케이스, 경쟁 조건, 통합 이슈는 수동 리뷰나 프로덕션 사용 중에만 나타나는 경우가 많습니다.
- **복잡한 리팩토링에는 사람의 안내가 필요할 수 있습니다** — 기능이 많은 파일을 건드리거나 암묵적인 관례를 이해해야 할 때, Coder가 올바르지만 최적이 아닌 코드를 생성할 수 있습니다.

**올바른 멘탈 모델**: 4x는 테스트 커버리지와 리뷰 피드백이 있는 탄탄한 초안을 제공합니다. 지시를 정확하게 따르지만 때로는 방향 제시가 필요한 유능한 주니어 개발자로 생각하세요.

### 프로젝트별 역할 커스터마이즈

4x는 상태 전환과 역할 전환만 처리합니다 — 프로젝트를 어떻게 빌드하고, 테스트하고, 리뷰해야 하는지는 알지 못합니다. 그 지식은 프로젝트 설정에 있습니다.

각 역할은 프로젝트의 `.4x/settings.json`을 읽어 무엇을 해야 할지 파악합니다. 컨텍스트를 많이 제공할수록 출력이 더 좋아집니다:

```json
{
  "project": {
    "name": "my-api",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["golangci-lint run"],
    "rules": ["모든 exported 함수에 GoDoc 주석 필수"]
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": {
      "model": "sonnet",
      "instructions": ["항상 생성자를 통한 의존성 주입 사용"]
    },
    "reviewer": {
      "model": "sonnet",
      "deep_model": "opus",
      "instructions": ["모든 쿼리 빌더에서 SQL 인젝션 확인"]
    },
    "tester": {
      "model": "sonnet",
      "instructions": ["통합 테스트에 mock이 아닌 testcontainers 사용"]
    }
  }
}
```

주요 필드:

| 필드 | 효과 |
|---|---|
| `project.build/test/lint` | Coder가 변경 후 실행; Tester가 검증에 `test` 사용 |
| `project.rules` | 모든 역할에 하드 제약으로 주입 |
| `roles.*.instructions` | 역할별 지침 — 집중할 것과 피할 것 |
| `roles.*.includes` | 읽을 추가 파일 (예: `["docs/api-conventions.md"]`) |

이 없이는 역할이 일반적인 동작으로 폴백합니다. 이를 통해 Designer가 아키텍처에 맞는 스펙을 작성하고, Coder가 컨벤션을 따르며, Reviewer가 프로젝트 특유의 함정을 잡아내고, Tester가 실제로 환경에서 실행되는 테스트를 작성합니다.

전체 레퍼런스는 [설정](configuration.md)을 참조하세요.

---

## 전체 워크플로우 (CLI 전용)

위와 동일한 흐름이지만 CLI 명령어를 직접 사용합니다 — AI 에이전트 세션 중이 아닐 때 유용합니다.

### Step 1: 작업 생성

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

필요하면 `.4x/features/F001-add-redis-cache-for-or.yaml`을 편집하여 description, priority, depends, repos 등의 필드를 보충합니다.

### Step 2: 루프 실행

```bash
# dry run으로 먼저 프롬프트 확인 권장
4x run F001 --dry-run

# 정식 실행
4x run F001 --runner claude
```

대시보드를 열어 실시간으로 관찰할 수 있습니다:

```bash
4x live -w   # 다른 터미널에서
```

### Step 3: 루프 완료 → pending-review

루프 완료 후 기능은 `pending-review`에 멈춥니다 — 이것은 의도된 것입니다. AI가 작업을 완료했지만 당신의 리뷰가 필요합니다.

```bash
4x status F001
# Phase: pending-review
```

### Step 4: 수동 리뷰

AI가 만든 결과물을 확인합니다:

```bash
# 최종 보고서 확인
cat .4x/F001/final-report.md

# 커밋 계획 확인
cat .4x/F001/commit-plan.md

# 코드 디프 확인
git diff                          # 비 worktree 모드
git diff main...4x/F001-add-redis  # worktree 모드
```

만족스럽지 않으면:

```bash
# 수동 수정 후 review + test 재실행
4x transition F001 --to reviewing
4x run F001

# 또는 처음부터 다시
4x transition F001 --to designing
4x run F001
```

### Step 5: 머지 & 정리

**비 worktree 모드** (변경 사항이 working tree에 직접 있음):

```bash
# 만족하면 완료 표시
4x done F001

# commit-plan.md에 따라 커밋
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Worktree 모드** (변경 사항이 독립 브랜치에 있음):

```bash
# 완료 표시
4x done F001

# 메인 브랜치에 머지
git merge 4x/F001-add-redis-cache-for-or

# worktree와 브랜치 정리
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### 흐름 요약

```
4x new "..."                     # 작업 생성
    ↓
4x run F001 --runner claude      # AI가 자동으로 Design→Code→Review→Test 수행
    ↓
pending-review                   # 당신의 리뷰를 기다림
    ↓
review final-report / diff       # 결과물 확인
    ↓
4x done F001                     # 완료 표시
    ↓
git merge + cleanup              # 머지, worktree/브랜치 정리
```

---

## 좋은 기능 설명 작성하기

기능 설명은 Designer의 유일한 입력입니다 — 명확하게 작성할수록 더 정확한 스펙이 나옵니다.

```bash
# 나쁜 예: 너무 모호하여 Designer가 스스로 추측
4x new "성능 개선"

# 좋은 예: 명확한 목표, 경계, 인수 조건
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

설명에 포함하면 좋은 것:
- **무엇을 할 것인가** (구체적 기능 또는 수정)
- **왜 하는가** (비즈니스 동기 또는 문제 설명)
- **경계** (건드리지 않을 것, 알려진 제한 사항)
- **인수 기준** (정량화 가능한 성공 정의)

## 기능 세분화

하나의 기능은 독립적으로 인도 가능한 하나의 변경에 대응합니다. 너무 크면 Coder가 길을 잃고, Reviewer가 놓치고, Test가 검증하기 어렵습니다.

| 세분화 | 적합 | 부적합 |
|---|---|---|
| API 엔드포인트 하나 | OK | — |
| 리팩토링 하나 (이름 변경, 인터페이스 추출) | OK | — |
| 버그 수정 하나 | OK | — |
| 전체 모듈을 처음부터 구축 | — | 여러 기능 + depends로 분할 |
| 3개 리포에 걸친 대형 기능 | — | 리포당 하나의 기능, depends로 연결 |

`depends`를 활용하여 큰 작업을 분해하세요:

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## Dry Run을 먼저 실행하세요

새 기능을 처음 사용하거나 설정을 변경한 후에는 `--dry-run`으로 프롬프트가 적절한지 확인하세요:

```bash
4x run F001 --dry-run
```

이는 네 역할의 전체 프롬프트를 출력하지만 LLM을 호출하지 않으며, 다음을 확인할 수 있습니다:
- Designer가 충분한 컨텍스트를 받았는지
- 프로젝트 규칙이 올바르게 주입되었는지
- 로케일이 올바른지

## 모델 선택 권장

| 역할 | 권장 | 이유 |
|---|---|---|
| Designer | opus 또는 동등 수준 | 요구사항의 깊은 이해와 아키텍처 분해 필요 |
| Coder | sonnet 또는 동등 수준 | 산출량이 많지만 최강 추론이 필요하지 않음 |
| Reviewer (체크리스트) | sonnet | 규칙 기반 검사, 속도 우선 |
| Reviewer (적대적) | opus | 숨겨진 버그를 찾기 위한 깊은 추론 필요 |
| Tester | sonnet | 테스트 작성과 검증 실행, 최강 추론이 필요하지 않음 |

설정 방법:

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

프로젝트가 간단하다면(작은 버그 수정, 작은 리팩토링) 전체를 sonnet으로 해도 됩니다. 비용 절약.

## 라운드 조정

기본 5라운드는 대부분의 경우에 적합합니다. 기능 복잡도에 따라 조정하세요:

| 시나리오 | 권장 라운드 |
|---|---|
| 간단한 버그 수정, 소규모 변경 | 2-3 |
| 일반적인 기능 개발 | 5 (기본값) |
| 복잡한 크로스 모듈 기능 | 7-10 |

```bash
4x run F001 --max-rounds 3   # 간단한 작업
4x run F001 --max-rounds 8   # 복잡한 작업
```

참고: 루프는 연속 3라운드 진행이 없으면 자동 중지됩니다(max-rounds를 다 채울 필요 없음).

## 리뷰 실패 대응

리뷰 실패(판정 FAIL 또는 CRITICAL 발견)는 자동으로 Coder에게 수정을 보내며 수동 개입이 필요 없습니다. 하지만 반복적으로 실패하면:

1. **review-report.md 확인** — `.4x/run/{feature-id}/rounds/round-{N}/review-report.md`
2. **coder-report.md 확인** — Coder가 문제를 이해했는지
3. **조정 고려**:
   - 기능 설명이 너무 모호 → 설명을 다시 작성하고 Designer 재실행
   - Reviewer가 너무 엄격 → `roles.reviewer.instructions`에서 특정 규칙 완화
   - 정말 어려운 문제 → 수동으로 수정하고 `4x transition`으로 진행

## 에스컬레이션 대응

Coder 또는 Tester가 스펙과 실제가 다름을 발견하면 자동으로 Designer에게 에스컬레이션합니다. 일반적인 시나리오:

- DB 스키마가 스펙과 다름 (`spec-mismatch`)
- 인수 기준이 불합리 (`criteria-wrong`)
- 외부 의존성 누락 (`blocker`)

에스컬레이션은 `.4x/run/{feature-id}/rounds/round-{N}/escalation.json`에 기록됩니다. Designer가 에스컬레이션 내용을 받고 스펙을 다시 작성합니다.

Designer도 해결할 수 없으면(보통 컨텍스트 부족) 루프가 `needs-attention`에 멈추며, 이때 수동 개입이 필요합니다:

```bash
# 상태 확인
4x status F001

# 스펙 또는 코드베이스를 수동으로 수정
vim .4x/F001/task-brief.md

# coding으로 다시 진행
4x transition F001 --to coding
```

## 중단된 기능 재개

4x는 파일 기반이므로 — 세션이 끊기거나 머신이 재부팅되어도 상태가 `.4x/`에 남아 있습니다. 다시 실행하면 됩니다:

```bash
4x run F001 --runner claude
```

마지막 단계와 라운드에서 이어서 진행하며, 처음부터 다시 시작하지 않습니다.

## Worktree 격리

여러 기능을 동시에 실행하거나 AI의 수정 사항을 격리하고 싶다면 worktree를 활성화하세요:

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

효과:
- 브랜치를 만들기 전, 각 저장소의 현재 브랜치에 upstream tracking branch가 설정되어 있으면 먼저 fetch 후 fast-forward합니다 — 로컬이 이미 최신이면 no-op이고, 원격과 분기된 경우에도 no-op(경고만 출력)이라 push되지 않은 로컬 커밋을 덮어쓰지 않습니다
- 각 기능이 `.worktrees/4x/{feature-id}/`에서 독립적으로 작업
- 자동으로 브랜치 `4x/{feature-id}` 생성
- 완료 후 머지 명령어 안내

```bash
# 완료 후 머지
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
```

## 배치 사용 시기

| 시나리오 | `4x run` 사용 | `4x batch run` 사용 |
|---|---|---|
| 기능 하나 수행 | OK | — |
| 의존성 있는 여러 기능 수행 | 수동으로 순서 정렬 필요 | 자동으로 의존성 순서 처리 |
| 밤새 백로그 소화 | — | OK, `batch stop`으로 언제든 중지 |

배치의 커밋 전략은 `"never"`로 고정됩니다 — 모든 변경 사항이 working tree에 있으며, 완료 후 수동 리뷰 후 커밋합니다.

## 대시보드 사용 시나리오

```bash
# 대시보드를 열고 기능 실행, 실시간 로그 확인
4x live -w &
4x run F001 --runner claude

# 대시보드에서 직접 기능 시작 (터미널 불필요)
# POST /api/run과 웹 UI 연동

# 다중 프로젝트 모니터링
4x live /path/to/project-a /path/to/project-b -w
```

## 로케일 설정

AI가 당신의 언어로 응답하게 합니다:

```bash
4x config set locale zh-TW
```

설정하지 않아도 됩니다 — `LANG` 환경 변수에서 자동으로 추론합니다.

## 트러블슈팅

### 기능이 needs-attention에 멈춤

특정 단계에서 필수 아티팩트가 누락되었음을 의미합니다(예: Designer가 task-brief.md를 생성하지 않음).

```bash
4x status F001          # 뭐가 빠졌는지 확인
4x check F001           # 전체 검사 실행
```

수동으로 파일을 보충하거나 해당 단계를 재실행합니다:

```bash
4x transition F001 --to designing
4x run F001
```

### 기능이 blocked에 멈춤

보통 러너 종료 코드 1(소프트 실패)입니다. 로그를 확인하세요:

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

해결 후 다시 진행합니다:

```bash
4x transition F001 --to coding
4x run F001
```

### 의존성 게이트에 의한 차단

```
blocked: F001-user-model is not done (status: coding)
```

의존되는 기능을 먼저 완료하거나 수동으로 표시합니다:

```bash
4x done F001
4x run F002
```

## E2E 테스트를 위한 gstack Browse 통합

[gstack](https://github.com/garrytan/gstack)은 4x의 Playwright 기반 e2e 테스트를 가속화할 수 있는 지속형 헤드리스 브라우저 데몬을 제공합니다. 매 테스트 라운드마다 Chromium을 콜드 스타트하는 대신(~3-5초), 데몬이 브라우저를 계속 실행 상태로 유지하고 라운드 간 로그인 세션을 보존합니다.

이 기능은 **선택 사항**입니다 — 4x의 내장 `web` 테스트 프로파일은 gstack 없이도 동작합니다. 데몬이 가장 유용한 경우:

- 프로젝트에 로그인이 필요한 경우 (세션 유지로 매 라운드 재인증 불필요)
- 여러 기능을 배치로 실행하는 경우 (모두 하나의 브라우저 인스턴스 공유)
- 콜드 스타트 지연 대신 200ms 미만의 브라우저 응답 시간을 원하는 경우

### 설정

1. gstack을 Claude Code 스킬로 설치합니다:

```bash
git clone --depth 1 https://github.com/garrytan/gstack.git ~/.claude/skills/gstack
cd ~/.claude/skills/gstack && ./setup
```

2. browse 데몬을 시작합니다 (백그라운드에서 실행됨):

```bash
# Claude Code에서
/browse-open http://localhost:4567
```

또는 수동으로 시작합니다:

```bash
cd ~/.claude/skills/gstack && bun run browse/src/server.ts
```

데몬은 랜덤 포트를 선택하고 연결 정보를 `.gstack/browse.json`에 기록합니다.

### 4x가 gstack browse를 사용하도록 설정

`.4x/settings.json`에서 내장 `web` 테스트 프로파일을 재정의합니다:

```json
{
  "test_profiles": {
    "web": {
      "include": "docs/test-profiles/gstack-web.md"
    }
  }
}
```

`docs/test-profiles/gstack-web.md`를 생성합니다:

```markdown
Web UI E2E Testing with gstack Browse:

- Use gstack browse daemon instead of launching a standalone Playwright instance
- Read connection info from .gstack/browse.json (port + auth token)
- Send commands via HTTP POST to the daemon:
  - `POST /command` with `{"command": "goto", "args": ["http://localhost:4567"]}`
  - `POST /command` with `{"command": "snapshot"}` to get the accessibility tree with @e refs
  - `POST /command` with `{"command": "click", "args": ["@e5"]}` to interact with elements
  - `POST /command` with `{"command": "screenshot"}` to capture evidence
- Include Bearer token from browse.json in all requests
- Save screenshots to the configured screenshot_dir
- Each AC item must have at least one screenshot as evidence
- Do NOT launch a separate Chromium instance — use the running daemon
- If the daemon is not running, fall back to standard Playwright (npx playwright test)
```

### 예시: gstack을 사용하는 test-strategy.yaml

```yaml
web: true
api: false
coder_only: false
profiles:
  - web
verify_commands:
  - "make build"
  - "make test"
```

Designer가 `profiles: [web]`을 지정하고 `test_profiles.web` 재정의가 gstack을 가리키도록 설정되어 있으면, Tester는 자동으로 gstack 전용 지침을 프롬프트에 주입받습니다.

### 로그인이 필요한 프로젝트에서

인증이 필요한 프로젝트(예: 관리자 대시보드)의 경우, `4x run`을 시작하기 전에 gstack을 통해 한 번 로그인합니다:

```bash
# gstack 데몬에서 로그인 페이지 열기
/browse-open https://your-app.example.com/login

# 수동으로 또는 gstack fill 명령으로 로그인
# 세션 쿠키는 이후 모든 4x 테스트 라운드에 걸쳐 유지됩니다
```

이후 Tester는 로그인 단계를 완전히 건너뛸 수 있습니다 — 데몬의 브라우저에 이미 유효한 세션이 있기 때문입니다.

### gstack을 사용하지 않는 경우

gstack을 사용하지 않으면 내장 `web` 프로파일이 별도 설정 없이 바로 동작합니다:

- Tester가 테스트 라운드마다 독립된 Playwright 인스턴스를 실행
- 임시 워크스페이스 생성 후 랜덤 포트에서 `4x live` 시작
- 테스트 실행, 스크린샷 촬영, 정리
- 라운드 간 상태 유지 없음 (매 라운드 클린 스타트)

프로파일 재정의에 대한 자세한 내용은 [테스트 프로파일](concepts.md#test-profiles)을 참조하세요.

---

## AI Agent에게 4x 한 번만 가르치기

기본적으로 새 AI 대화를 시작할 때마다 4x 문서를 처음부터 다시 읽습니다. **글로벌 지시 파일**을 추가하면 이 과정을 없앨 수 있습니다. 대화 시작 전부터 에이전트가 4x 명령어, 디렉토리 구조, 역할 계약을 이미 알고 있게 됩니다.

### Claude Code

`~/.claude/rules/4x.md`에 4x 빠른 참조를 만드세요 (아래 예시 참조). `~/.claude/rules/` 안의 파일은 모든 세션에 자동으로 로드됩니다.

### Gemini CLI

`~/.gemini/instructions/4x.md`에 동일한 내용을 만드세요.

### Codex

글로벌 `AGENTS.md`에 4x 지시사항을 추가하세요.

### 예시: 글로벌 규칙용 4x 빠른 참조

[`docs/reference/4x-agent-rules.md`](../../reference/4x-agent-rules.md)를 에이전트의 글로벌 규칙 디렉토리에 복사하세요. 다음 내용이 포함되어 있습니다:

- 모든 CLI 명령어와 자주 쓰는 플래그
- `.4x/` 디렉토리 구조
- 역할 계약 (읽기 / 쓰기 / 제약)
- 상태 머신 전환
- 지원되는 러너
