# 핵심 개념

## 네 가지 역할

| 역할 | 책임 | 입력 | 출력 | 제한 |
|---|---|---|---|---|
| **Designer** | 요구사항 분석, 스펙 작성, 인수 기준 및 테스트 전략 정의 | 기능 설명, 코드베이스 | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | 소스 코드 수정 불가 |
| **Coder** | 스펙에 명시된 대로 구현 | `task-brief.md`, 이전 테스트/리뷰 보고서 | 소스 코드, `coder-report.md` | 인수 기준 또는 테스트 스크립트 수정 불가 |
| **Reviewer** | 버그, 보안 문제, 스펙 위반 포착 | 디프, 스펙, 코더 보고서, 프로젝트 규칙 | `review-report.md` | 소스 코드 수정 불가 |
| **Tester** | 증거를 기반으로 인수 기준 검증 | 인수 기준, 코더 보고서, 테스트 전략 | 테스트 스크립트, `test-report.md`, `verify.json`, `final-report.md` | 소스 코드 수정 불가 |

각 역할은 **격리**되어 있습니다 — Coder는 구현 중에 이전 리뷰 피드백을 보지 못합니다. Tester는 Coder가 아닌 Designer가 작성한 기준으로 검증합니다.

### 추가 루프 역할

두 가지 추가 역할이 루프 후반부에서 동작합니다:

| 역할 | 단계 | 책임 |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | 적대적 리뷰 — 전체 디프에서 최악의 버그를 찾아냅니다 |
| **Acceptor** | `accepting` | 미해결 이슈를 종합하여 `final-report.md`를 작성하고 사람의 리뷰를 대기합니다 |

Acceptor는 자체 전용 모델 설정(`roles.acceptor.model`)을 사용하며 — Designer와 구분됩니다. 모든 라운드의 보고서를 전부 다시 읽는 대신, 마지막 라운드의 review/test/deep-review 보고서와 escalation을 읽어 미해결 이슈를 추려냅니다.

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
| **sub-reviewer** (xN) | 딥 모델 (`roles.reviewer.deep_model`) | 디프 + 할당된 관점 하위 집합 | `deep-review-partial-{i}.md` |
| **synthesizer** | synthesizer 모델 (`roles.synthesizer.model`, 기본값 `sonnet` 티어) | 모든 부분 보고서의 전체 내용 | `deep-review-report.md` |

관점은 균등하게 겹침 없이 분할됩니다: 기본 `parallel_reviewers: 3`에서 그룹은 `[1-4]`, `[5-8]`, `[9-11]`(정확성 / 품질+관례 / 이력+피드백)입니다. `roles.deep-reviewer.angles_per_reviewer`를 설정하면 그룹 크기를 명시적으로 지정할 수 있으며, 비워두면 자동으로 `ceil(11/N)` 균형이 적용됩니다. N개의 sub-reviewer가 병렬로 실행된 후, 단일 synthesizer가 중복을 제거하고 충돌을 중재하며 신뢰도 점수를 통합하여 자가 치유 루프와 `parseReviewVerdict`가 이미 사용하는 것과 동일한 `deep-review-report.md` 형식으로 통합합니다 — 따라서 하위 단계는 모두 변경 없이 동작합니다.

`parallel_reviewers`가 설정되지 않았거나 `<= 1`이면, 루프는 원래의 단일 에이전트 흐름으로 폴백합니다: 한 명의 딥 리뷰어가 11개 관점을 모두 렌더링하고 `deep-review-report.md`를 직접 작성하며, 부분 보고서나 synthesizer는 없습니다.

### 선택적 딥 리뷰 관점

딥 리뷰를 배포하기 전에, 4x는 diff 영향을 받는 파일 경로를 분석하여 11개의 관점 중 어떤 것을 실행할지 선택합니다. `roles.deep-reviewer`의 `angle_mapping` 필드는 경로 접두사(예: `internal/state/`)와 접미사 패턴(예: `*_test.go`)을 관점 번호에 매핑합니다. 변경된 각 파일에 대해 가장 길게 매칭되는 접두사가 우선합니다(접두사 규칙이 접미사 규칙보다 우선); 매칭된 관점들의 합집합이 선택된 세트가 됩니다. 어떤 파일도 규칙에 매칭되지 않으면 안전 폴백으로 11개 관점 모두 실행됩니다.

선택 결과는 라운드 디렉토리의 `deep-review-angles.json`에 기록되며, 어떤 파일이 어떤 규칙에 매칭되었는지, 각 파일이 어떤 관점에 기여했는지 포함됩니다. 이 아티팩트는 크래시 복구 시 올바른 부분 카운트를 결정하는 데도 사용됩니다.

매핑에 관계없이 11개 관점 모두를 강제 실행하려면:
- `4x run`에 `--all-angles`를 전달
- 기능 YAML에 `deep_review_all_angles: true` 설정

`angle_mapping`은 `settings.json`의 `roles.deep-reviewer` 아래에서 커스터마이즈할 수 있습니다; 설정하지 않으면 표준 프로젝트 레이아웃(`internal/state/`, `internal/protocol/`, `cmd/`, `docs/`, `templates/`, `dashboard/`, `*_test.go`)을 다루는 내장 기본값이 적용됩니다.

### 딥 리뷰 서브페이즈 & 크래시 복구

`deep-reviewing` 단계는 내부적으로 여러 단계(sub-reviewer → synthesizer → mini-coder → re-verifier)를 실행하지만, 이들은 **상태 머신 단계가 아닙니다**. 라이브 진행 상황과 크래시 복구가 *어느 단계가 실행 중인지* 인식할 수 있도록, `State`는 `subPhase` 필드(`internal/protocol/state.go`)를 가지며 이는 `phase == deep-reviewing`일 때만 의미가 있습니다:

| `subPhase` | 단계 | 설정 시점 |
|---|---|---|
| `reviewing` | sub-reviewer(또는 단일 에이전트 폴백)가 diff를 스캔 중 | deep review 진입 시 |
| `synthesizing` | synthesizer가 부분 보고서를 병합 중 | synthesizer 생성 시 |
| `fixing` | mini-coder가 차단 이슈를 수정 중 | self-heal mini-coder 생성 시 |
| `reverifying` | re-verifier가 수정을 확인 중 | self-heal re-verifier 생성 시 |

`WriteState`는 단일 불변식을 강제합니다: `phase`가 `deep-reviewing`이 아닌 쓰기는 `subPhase`를 빈 문자열로 초기화합니다(`omitempty`로 `state.json`에서 완전히 제외). 따라서 deep review를 떠날 때 — `accepting`, `amending`, `needs-attention` 어느 경로로 나가든 — 오래된 sub-phase가 남지 않습니다.

크래시 복구 시, `smartResumePhase`는 `deep-review-report.md`가 불완전한 경우 deep review를 처음부터 재시작하지 않습니다. 디스크 아티팩트를 검사하여 올바른 단계에서 재개합니다:

- **`deep-review-partial-{i}.md`가 누락되거나 불완전한 경우** → `reviewing`에서 재개; 병렬 루프는 부분 파일이 누락된 sub-reviewer만 재시작하며(`missingDeepPartials`), 각 인덱스의 원래 관점 그룹을 재사용하여 재할당 없이 진행합니다.
- **모든 부분 파일이 있지만 보고서가 불완전한 경우** → `synthesizing`에서 재개; sub-reviewer는 건너뛰고 synthesizer만 재실행합니다.
- **보고서는 완전하지만 FAIL인 경우** → 변경 없이 `subPhase`를 초기화하고 `amending`으로 라우팅합니다.

부분 파일은 `deepPartialComplete`로 완전 여부를 판단합니다 — 파일이 존재하고 비어있지 않으며, deep-reviewer 템플릿이 항상 출력하는 `## Statistics` 센티넬 섹션을 포함하는지 확인하여 반쯤 쓰인 부분 파일을 완전한 것으로 오인하지 않습니다. 이 최소 재실행 복구는 이미 완료된 단계에 (비용이 큰) deep 모델을 재소비하는 것을 방지합니다.

### 자동 발견 기능

딥 리뷰어는 실제 문제이지만 **현재 기능의 범위 밖**인 이슈를 종종 발견합니다 — 잠재적 버그, 기술 부채, 누락된 기능 등. 기록할 곳이 없으면 이런 메모는 보고서에 묻히게 됩니다. `auto_discover_features`가 활성화되면 실행 루프가 자동으로 이를 캡처합니다.

딥 리뷰어는 범위 밖 후보를 `deep-review-report.md`의 `## Discovered Issues` 섹션에 `[NEW-FEATURE] <제목>` 블록(짧은 설명 포함)으로 작성합니다. **최종 딥 리뷰 PASS** 후(`accepting`에 도달하는 두 가지 경로만 — 첫 번째 PASS와 자가 치유 re-verifier의 PASS 전환), 루프는 이 블록을 파싱하고 CLI 계층에서 완전히(LLM 호출 없이) 처리합니다:

- 각 후보를 기존 기능 및 이미 유지된 후보와 Jaccard 토큰 유사도 검사로 **중복 제거**합니다.
- `max_discovered_features`(기본값 `3`)까지 **수량 제한**하며, 나머지는 제한됨으로 기록됩니다.
- 유지된 후보를 새 기능 YAML(상태 `not-started`, `4x new`와 동일한 번호 체계)로 **생성**하고, 생성 시마다 `feature-discovered` 이벤트를 추가합니다.
- 결과(생성됨 / 중복으로 건너뜀 / 제한됨)를 `.4x/run/{feature-id}/discovered-features.md`에 **요약**합니다.

이 단계는 최선의 노력으로 수행됩니다: 오류가 발생해도 `accepting`으로의 전환을 차단하지 않습니다. 최종 딥 리뷰 PASS에서만 실행됩니다 — 중간 라운드와 FAIL/`needs-attention` 경로에서는 실행되지 않습니다. 설정에 대해서는 [설정 → 자동 발견 기능](configuration.md#auto-discover-features)을 참조하세요.

### 히스토리 마이너 & 후보 풀

자동 발견 기능은 **최종 딥 리뷰 PASS**에서만 동작하며, 해당 라운드의 `deep-review-report.md`에서 `[NEW-FEATURE]` 블록만 파싱합니다. 가장 풍부한 신호인 *실패* — `escalation.json`, `needs-attention`/`abandoned`/`blocked`에 멈춘 기능, 여러 기능에 걸쳐 반복되는 동일한 리뷰어 FAIL 이슈 — 는 수집되지 않습니다.

`4x mine` 명령어가 이 공백을 메웁니다. `.4x/` 전체 디렉토리를 스캔하여 과거 실패 신호를 수집하고 `.4x/candidates.json`에 후보 풀로 집계합니다. 순수 CLI/프로토콜 계층 명령어로 — **LLM 호출 없음**, 자동 발견 기능이 사용하는 것과 동일한 Jaccard 토큰 유사도 중복 제거만 수행합니다. 세 가지 스캐너가 풀에 데이터를 공급하며 각 후보에 `Source`와 `Origin` 추적 문자열을 태그합니다:

| 소스 | 신호 | Origin 형식 |
|---|---|---|
| `escalation` | `needed: true`인 각 라운드의 `escalation.json`, `reason`으로 분류(spec-mismatch / criteria-wrong / blocker / scope-change) | `<featureID> round-<n> <reason>` |
| `stuck` | `state.json` 단계가 `needs-attention`, `abandoned`, `blocked`인 기능; 차단 사유는 `stopReason`/`stopMessage`에서 가져오며, 없으면 최신 라운드의 에스컬레이션 `detail`로 폴백 | `<featureID> <phase>` |
| `fail-pattern` | **distinct** 기능에 걸쳐 반복되는 리뷰어/딥 리뷰어 FAIL 이슈 제목(같은 기능의 여러 라운드는 한 번으로 계산), Jaccard 유사도로 클러스터링되며 `--min-occurrences`(기본값 `3`)로 게이팅 | `N features: <ids>` |

반복 fail-pattern은 리뷰 체크리스트나 템플릿으로 승격할 것을 제안하는 `CandidateLearning`(카테고리 `review`)도 생성합니다.

출력 `CandidatePool`(`candidates.json`)에는 `Version`, `GeneratedAt`, `Candidate` 목록, `CandidateLearning` 목록이 포함됩니다. 기록 전에 후보는 세 가지 방식으로 중복 제거됩니다: 기존 기능 YAML, 이전 `candidates.json`, 현재 배치 내에서. 플래그: `--min-occurrences`(fail-pattern 임계값), `--output`(기본값 `.4x/candidates.json`), `--dry-run`(기록 없이 요약만 출력).

명령어 전체는 최선의 노력으로 수행됩니다 — 손상된 기능 하나는 로그에 기록되고 건너뛰며, 스캔을 중단하지 않습니다. 중요하게, `4x mine`은 **후보 풀만 생성하며 기능을 생성하지 않습니다**. 후보가 실제 기능으로 승격될지는 별도의 gate(F097)에 맡깁니다.

### Evolve Driver

`4x evolve`는 mine, F097 value gate, enrichment를 하나의 반복 실행 가능한 폐쇄 루프로 연결합니다: **mine → gate (pre → gate LLM 역할 → post) → enrich → enqueue → (선택) auto-run → learnings가 다음 라운드에 피드백**. CLI 레이어는 LLM에 접근하지 않습니다 — gate 역할과 enrichment 모두 `runner.Runner` 하위 프로세스로 실행되며, 인라인 API 호출은 없습니다.

파이프라인 순서는 **mine → gate → enrich → enqueue**입니다(mine → enrich → gate가 아닙니다): gate는 가공되지 않은 `Candidate`를 소비하므로, enrichment — 후보를 완전한 `feature.Feature`로 구현하는 처리 — 는 gate 생존자에 대해서만 실행되며, 거부된 후보에 LLM 비용을 낭비하지 않습니다. 통과한 후보는 `not-started` feature YAML로 대기열에 넣어집니다(value gate 통과**가 곧** 승인이며, draft→not-started의 두 번째 단계는 없습니다). enrichment가 실패하거나 폐기된 경우에도 후보는 설명 텍스트로 생성된 기본 feature로 대기열에 넣어지며 `enriched=false`로 표시됩니다 — gate가 이미 그 가치를 보증했습니다.

각 호출은 정확히 **1라운드**를 실행합니다. 반복 라운드는 외부 구동(cron 또는 반복 호출)입니다. 각 라운드는 `.4x/evolve-report.md`(Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted)에 기록되며, Dashboard가 `GET /api/evolve-report`를 통해 표시합니다.

**공회전 방지 중단**은 성과 없이 루프가 영원히 도는 것을 방지합니다. `.4x/evolve-state.json`은 호출 간에 `consecutiveNoAccept`를 영속화합니다. 아무것도 수락하지 않은 라운드는 증가시키고, 무엇이든 수락한 라운드는 0으로 리셋합니다. `evolution.max_idle_rounds`에 도달하면 다음 호출이 마이닝 전에 중단되고 보고서를 `Halted`로 표시하며 exit 0으로 종료합니다. 이 설정은 **미설정**(`nil` → 기본값 `3`)과 명시적 `<= 0`(중단 비활성화 — 항상 실행)을 구별합니다. `--force`는 한 번의 중단을 재정의합니다.

`--auto-run`을 사용하면 대기열에 넣은 각 feature의 메타 루프가 즉시 실행되며, 항상 F098 self-mod scope guard 하에서 동작합니다: `self_mod_guard.protected_paths`에 접근하는 승인되지 않은 feature는 자동 완료되지 않고 보고서에서 `SelfModBlocked`로 표시됩니다(`4x done --approve-self-mod`로 해제). `--dry-run`은 읽기 전용 — mine/dedupe 요약을 출력하고, 아무것도 기록하지 않고, runner를 시작하지 않고, feature를 생성하지 않습니다(`--auto-run`이 있으면 경고와 함께 무시합니다).

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
└── run/                            # 런타임 산출물 (feature별 작업 디렉토리)
    └── {feature-id}/
        ├── state.json                   # 단계, 역할, 라운드, 활성, 러너, runners, 중지 사유, 프로파일
        ├── events.jsonl                 # 감사 추적
        ├── baseline.json                # 코딩 전 스냅샷 (HEAD, 브랜치, dirty 파일)
        ├── task-brief.md                # Designer → Coder: 스펙 + 아키텍처
        ├── acceptance-criteria.md       # Designer → Tester: 테스트 가능한 기준
        ├── test-strategy.yaml           # Designer → Tester: 테스트 접근법
        ├── final-report.md              # 루프 종료 요약
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

### 워크트리 경로 복구

기능이 worktree 격리 모드로 실행될 때, 루프는 시작 시 `worktree: <경로>`를 출력하며 이는 `events.jsonl`에 `run-output` 이벤트로 기록됩니다. `Workspace.WorktreePath`는 git을 재실행하는 대신 감사 추적을 스캔하여 나중에(예: 스크린샷 탐색 시) 이 경로를 복구합니다.

스캔은 `events.jsonl` **전체**를 읽고 **마지막** 매칭 `run-output` 이벤트의 경로를 반환합니다. 이는 재실행 시 중요합니다: 각 `4x run`이 새 `worktree: …` 이벤트를 추가하므로, 파일에는 기능의 수명 동안 항목이 누적됩니다. 처음 몇 줄만 읽으면 충분한 이벤트가 쌓인 후 경로를 놓치거나, 이미 제거된 오래된 worktree 경로를 반환할 수 있습니다. 마지막 매칭 항목을 사용하면 항상 가장 최근 실행의 worktree를 얻을 수 있습니다.

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
| **빌드 게이트** | coding/amending 단계에서: `settings.json`의 빌드 + 린트 명령어를 실행하고 `build-gate.json`을 기록. 실패 시 라운드 차단; Coder 에이전트가 수정 후 `4x check`를 재실행해야 함 |
| **Testing → Accepting 게이트** | `verify.json`(passed=true), `test-report.md`, `final-report.md` 필요. `test-strategy.yaml`에 `manual_checks`가 정의된 경우, 각각에 비어있지 않은 증거가 담긴 `manual_check_results` 항목이 있어야 함 |
| **Self-mod guard** | Scope 위에 레이어로 추가됨(대체가 아님): 보호된 경로에 대한 파일 수준 변경을 플래그 처리하고, 라운드당 보호된 diff가 예산을 초과하면 라운드를 차단하며, 수락 전 수반되는 테스트를 요구하고, 수동 승인 전까지 자동 병합을 차단 |

`4x check <feature-id>`로 수동 실행 가능합니다.

### Self-mod guard

4x가 자기 자신에 대해 실행될 때(메타 루프), 핵심 기반(상태 머신 / 가드레일 / 프로토콜)에 대한 변경은 일반 기능 작업보다 위험합니다 — 여기서 발생하는 회귀는 전체 멀티 역할 루프를 망가뜨립니다. self-mod guard는 repo 수준의 Scope guard 위에 추가적인 레이어를 더하며, `settings.json`의 `self_mod_guard` 아래에서 설정합니다:

```json
"self_mod_guard": {
  "protected_paths": ["internal/state/", "internal/guard/", "internal/protocol/"],
  "max_diff_lines": 200,
  "require_tests": true
}
```

- `protected_paths` — 경로 접두사 허용 목록(범위 루트에 대한 상대 경로); 이 경로 아래의 변경이 플래그 처리됩니다. 설정하지 않으면 세 가지 아키텍처 레드라인으로 기본 설정됩니다.
- `max_diff_lines` — 라운드당 보호된 diff 예산; 초과 시 guard가 실패하고 기능이 `needs-attention`으로 이동합니다. 기본값 `200`.
- `require_tests` — `true`(기본값)일 때, 보호된 `.go` 변경은 기능이 `testing`을 벗어나기 전에 보호된 `_test.go` 변경을 함께 포함해야 합니다.

터치는 코딩 후 guard 검사 중 한 번 감지되어 `state.json`에 영속화됩니다(`selfModTouched` / `selfModPaths`). 보호된 경로를 터치하면 자동 병합이 되지 않습니다: `4x done` / `4x merge`는 `--approve-self-mod`로 재실행하기 전까지 차단되며, 이는 state에 `selfModApproved`를 기록합니다.

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

전체 stdout/stderr 출력은 `.4x/run/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`에 기록됩니다.

### 훅 병합 (`MergeHooks`)

글로벌과 기능 훅은 `MergeHooks`로 병합됩니다: 모든 글로벌 키를 복사한 후, 기능 키가 동일한 이름의 글로벌 키를 완전히 재정의합니다. 글로벌에만 있는 키는 보존됩니다. 둘 다 nil이면 nil을 반환합니다.

---

## 헬스 체크

Tester 역할이 시작되기 전에, CLI가 자동으로 환경이 정상인지 확인할 수 있습니다 — 빌드 통과 여부, 서비스 상태, 엔드포인트 응답 여부 등. 여기서 잡힌 환경 문제는 테스트 사이클의 낭비를 방지합니다. 헬스 체크는 AI 역할이 아닌 CLI가 실행하며, `testing` 단계 진입 시, `pre_testing` 훅 이후 Tester 러너 생성 전에만 실행됩니다.

### 설정

헬스 체크는 세 개의 필드를 가집니다(`internal/protocol/verify.go`의 `HealthCheck`):

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

Designer가 `test-strategy.yaml`에 프로파일을 나열합니다(`internal/protocol/verify.go`의 `TestStrategy.Profiles`):

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles`는 `omitempty`입니다 — 이 필드가 없는 `test-strategy.yaml`은 이전과 정확히 동일하게 동작합니다(주입 없음).

### 수동 확인

빌드/테스트/린트 이상의 런타임 검증이 필요한 AC 항목에 대해, Designer는 `test-strategy.yaml`에 `manual_checks`를 추가할 수 있습니다(`internal/protocol/verify.go`의 `TestStrategy.ManualChecks`):

```yaml
manual_checks:
  - id: mc-1
    ac_ref: AC-3
    description: "routing이 올바르게 분류됨을 검증"
    steps:
      - "server 시작: go run ./cmd/gate --port 8080"
      - "curl http://localhost:8080/health → 200 확인"
  - id: mc-2
    ac_ref: AC-5
    description: "graceful shutdown 검증"
    steps:
      - "server를 시작하고 SIGTERM 전송"
      - "exit code가 0임을 확인"
```

Tester는 각 단계를 실행하고 실제 출력을 `verify.json`의 `manual_check_results`(`VerifyEvidence.ManualCheckResults`)에 증거로 기록해야 합니다. guard는 수동 확인 결과가 없거나 증거가 비어있으면 `testing → accepting` 전환을 차단합니다. 실패가 재시도 가능한 경우, tester는 `guard-feedback.json`으로 guard 오류가 주입된 상태에서 자동으로 한 번 재시도합니다; 두 번째 실패 시 `needs-attention`으로 에스컬레이션합니다.

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
