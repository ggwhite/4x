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

## 전체 워크플로우

작업 생성부터 인도까지의 전체 흐름 — 4x가 AI 개발을 담당하고, 당신은 최종 리뷰와 머지를 담당합니다.

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

1. **review-report.md 확인** — `.4x/{feature-id}/rounds/round-{N}/review-report.md`
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

에스컬레이션은 `.4x/{feature-id}/rounds/round-{N}/escalation.json`에 기록됩니다. Designer가 에스컬레이션 내용을 받고 스펙을 다시 작성합니다.

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
