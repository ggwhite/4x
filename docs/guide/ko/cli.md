# CLI 레퍼런스

모든 feature-id 인수는 대소문자 구분 없는 접두사 매칭을 지원합니다. `4x run f001`, `4x run F001-user`, `4x run F001` 모두 `F001-user-authentication-w`로 해석됩니다. 모호한 접두사는 일치 항목을 나열하는 오류를 생성합니다.

---

## `4x init`

현재 디렉토리에 `.4x/` 워크스페이스를 초기화합니다.

```
4x init
```

- 프로젝트 언어와 빌드/테스트/린트 명령어를 자동 감지
- 6개의 기본 러너(claude, codex, gemini, agy, copilot, cursor)로 `~/.4x/settings.json` 생성
- `.4x/plugins/`에 내장된 플러그인 파일 배포
- 루트 레벨 파일에 `@import` 라인 추가 (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md, .cursorrules)
- `.4x/`가 이미 존재하면 오류 발생

### `4x init --dump-templates`

내장된 역할 프롬프트 템플릿을 `.4x/templates/`에 덤프하여 프로젝트에서 재정의할 수 있게 합니다.

```
4x init --dump-templates          # 내장 템플릿을 .4x/templates/에 기록
4x init --dump-templates --force  # 기존 템플릿 파일 덮어쓰기
```

- `.4x/`가 이미 존재해야 합니다 (먼저 `4x init` 실행)
- 내장된 모든 `*.md.tmpl`(`locale.tmpl` 포함)을 `.4x/templates/`에 기록
- `--force`를 지정하지 않으면 기존 파일은 경고와 함께 건너뜀
- 프롬프트 생성 시 `.4x/templates/{file}`이 내장 템플릿보다 우선합니다(전체 파일 재정의); `locale.tmpl`과 각 역할 템플릿은 독립적으로 폴백됩니다

---

## `4x new <title>`

선택적 메타데이터를 포함하여 새 기능을 생성합니다.

```
4x new "Feature title" [flags]
```

| 플래그 | 설명 |
|---|---|
| `--id` | 기능 ID의 커스텀 slug (자동 잘림 건너뜀) |
| `--desc` | 기능 설명 (기본값: 제목과 동일) |
| `--subtask` | `"id:name"` 형식의 하위 작업 (반복 가능); 첫 번째 콜론 앞이 id, 나머지 전체가 name (name에 콜론 포함 가능, 예: `10:00`, `group:artifact`, URL); description은 생성 후 YAML을 편집하여 설정 |
| `--rule` | 규칙 참조 (반복 가능) |
| `--depends` | 의존 기능 ID (반복 가능) |
| `--priority` | 우선순위 수준 (0=긴급, 1=높음, 2=보통, 3=낮음) |
| `--repo` | 범위에 포함할 리포지토리 (반복 가능) |
| `--json` | JSON 형식으로 출력 |

`.4x/features/F{NNN}-{slug}.yaml`을 `not-started` 상태로 생성합니다.
자동 생성된 slug는 단어 경계에서 잘립니다. `--id`로 재정의할 수 있습니다.
생성은 공유 `feature.Create` 경로를 통해 실행됩니다([개념](concepts.md#기능-생성) 참조) — 대시보드의 `POST /api/new`도 동일한 로직을 사용하므로, 여기 플래그들은 대시보드의 새 기능 폼과 1:1로 대응됩니다.

예시:
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

---

## `4x run <feature-id>`

기능에 대해 Design-Code-Review-Test 루프를 실행합니다.

```
4x run <feature-id> [flags]
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--runner` | 설정 기본값 | 러너 플러그인 이름 |
| `--max-rounds` | `5` | 최대 루프 반복 횟수 |
| `--timeout` | `3600` | 단계별 타임아웃 (초) |
| `--dry-run` | `false` | LLM 호출 없이 역할 프롬프트만 출력 |
| `--json` | `false` | 실행을 시작하고 JSON을 즉시 반환 |
| `--profile` | auto | 파이프라인 프로파일 (`full`/`normal`/`quick` 또는 커스텀); 우선순위 기반 자동 선택을 재정의 |

`--profile`은 어떤 역할이 실행될지 선택합니다. 기본 제공 프로파일: `full`(전체 6개 역할), `normal`(coder/reviewer/tester/acceptor), `quick`(coder/reviewer). 프로파일에 포함되지 않은 역할은 통과됩니다(러너를 호출하지 않고 합법적 경로를 따라 상태가 진행됨). 생략 시, `settings.json`에 `profiles` 섹션이 존재하면 기능의 우선순위에 따라 자동 선택되고, 그렇지 않으면 `full`입니다. 자세한 내용은 [설정 → 프로파일](configuration.md#프로파일)을 참조하세요.

루프는 다음을 수행합니다: init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review. 리뷰 실패 시 코드가 다시 수행됩니다. 테스트 실패 시 루프가 코딩으로 재진입합니다.

비 designer 러너가 완료될 때마다 가드레일 검사가 자동으로 시행됩니다(범위, 베이스라인, 필수 파일). 위반 시 기능이 `needs-attention`으로 전환되고 루프가 중단됩니다. Designer는 소스 코드를 수정하지 않으므로 면제됩니다.

리뷰 판정은 `PASS`로 시작해야 통과됩니다. `## Verdict` 헤딩과 판정 텍스트 사이의 빈 줄은 무시됩니다. 모호한 출력(`TODO`, `ERROR`, 깨진 텍스트, `## Verdict` 블록 누락)은 실패로 처리됩니다.

`settings.json` 또는 기능 YAML에 선언된 단계 훅은 루프 내 각 단계 전환 전후에 자동 실행됩니다. 설정 상세 내용은 [단계 훅](concepts.md#단계-훅)을 참조하세요.

`testing` 단계 진입 시(`pre_testing` 훅 이후, Tester 러너 생성 전), `health_check`가 설정되어 있으면 환경을 헬스 체크합니다. 검사 명령어가 순서대로 실행되고, 실패 시 recovery 명령어를 한 번 실행한 후 검사를 재시도합니다. 환경이 여전히 실패하면 기능이 `needs-attention`으로 전환되고 루프가 중단됩니다. 설정 상세 내용은 [헬스 체크](concepts.md#헬스-체크)를 참조하세요.

`settings.json`에서 `auto_discover_features`가 활성화되면, 최종 딥 리뷰 **PASS** 시 `deep-review-report.md`의 `[NEW-FEATURE]` 마커를 파싱하여 딥 리뷰어가 지적한 범위 밖 이슈에 대한 기능 YAML을 자동 생성합니다(중복 제거 및 수량 제한 적용). 자세한 내용은 [설정 → 자동 발견 기능](configuration.md#자동-발견-기능)과 [개념 → 자동 발견 기능](concepts.md#자동-발견-기능)을 참조하세요.

기능이 `blocked` 또는 `needs-attention` 단계에 있으면 현재 역할에 따라 적절한 재개 단계로 자동 복구합니다.

의존성 게이트를 자동 확인합니다 — 의존 기능이 완료되지 않으면 차단됩니다.

설정에 `isolation: "worktree"`가 설정된 경우 `.worktrees/4x/<feature-id>/` 아래의 git worktree에서 실행됩니다. 멀티 리포 모드(workspace.repos 설정 시)에서는 각 리포지토리가 `.worktrees/4x/<feature-id>/<repo-name>/` 아래에 자체 worktree를 갖고, 워크스페이스 수준 파일(go.work, Makefile 등)이 함께 복사됩니다. Coder 프롬프트에는 `== Workspace Repos ==` 섹션이 포함됩니다; worktree 모드에서 각 항목은 리포 이름을 상대 경로(예: `core → core/`)로 표시하여 coder가 올바른 디렉토리 경계 내에서 작업하도록 합니다.

---

## `4x status [feature-id]`

기능 상태를 표시합니다.

```
4x status              # 모든 기능, 상태별 그룹화
4x status <feature-id> # 단일 기능 상세 정보 및 하위 작업
4x status --pending    # done/abandoned 기능 숨기기
4x status --json       # JSON으로 출력
```

| 플래그 | 설명 |
|---|---|
| `--pending` | done/abandoned 기능 숨기기 |
| `--json` | JSON 형식으로 출력 |

그룹: Running, Review, Pending, Todo, Done (done은 최대 5개 표시). 백로그 드리프트 경고를 포함합니다.

단일 기능 상세(`4x status <feature-id>`)에서 스크린샷이 존재하면 다음도 출력합니다:

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x cost`

각 runner가 남긴 stream 로그에서 feature 전반의 run 비용을 집계합니다. 읽기 전용이며 run 데이터를 변경하지 않습니다.

```
4x cost                       # 모든 feature의 per-role 비용 표
4x cost --feature <id>        # 단일 feature의 per-round per-role 상세
4x cost --by-round            # 라운드별 비용 + retry(round>=2) 비중
4x cost --feature <id> --by-round  # 단일 feature의 라운드별 상세
4x cost --json                # 구조화된 출력(위 뷰 중 하나)
```

| Flag | 설명 |
|---|---|
| `--feature <id>` | 단일 feature로 필터링하고 per-round per-role 상세를 표시 |
| `--by-round` | 라운드별로 집계하고 retry(round>=2) 비중을 표시 |
| `--json` | JSON으로 출력 |

데이터 소스는 `logs/*.stream.jsonl`이 우선이며(role invocation당 한 파일, `total_cost_usd` 포함), 파일명에 round와 role이 인코딩됩니다. stream 로그가 전혀 없는 feature(오래된 run)는 `events.jsonl`의 `run-end` 이벤트를 보조로 사용합니다. `total_cost_usd` 필드가 없는 stream 로그는 건너뛰고 `Skipped N stream log(s)` 카운트로 보고하며 실패로 처리하지 않습니다.

기본 표는 총 비용 기준으로 정렬된 `ROLE / CALLS / TOTAL($) / AVG($) / PCT(%)`와 `TOTAL` 행을 표시합니다. `--by-round`는 `TYPE` 열(round 0–1은 `initial`, round≥2는 `retry`)을 추가하고 retry 비중을 USD와 백분율로 보고합니다.

---

## `4x subtask <feature-id> <subtask-id>`

기능 내 하위 작업의 상태를 업데이트합니다.

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| 플래그 | 설명 |
|---|---|
| `--status` | 새 상태: `done`, `in-progress`, `blocked`, `not-started`, `ready-for-review` (필수) |

예시:
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x approve <feature-id>`

enriched auto-discover가 생성한 `draft` 기능을 승인하여 `draft → not-started`로 전환합니다. 메타 루프가 이 기능을 처리하게 됩니다. Draft는 `enrich_discovered_features`가 활성화되고 `enrich_auto_approve`가 `false`일 때만 생성됩니다. 기능이 `draft` 상태가 아니면 오류가 발생합니다.

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

enriched auto-discover가 생성한 `draft` 기능을 거부하여 `draft → abandoned`로 전환합니다. 메타 루프에서 제외됩니다. 기능이 `draft` 상태가 아니면 오류가 발생합니다.

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

`needs-attention` 또는 `blocked` 상태에서 멈춘 기능을 다시 작업 단계로 전환하고 즉시 `4x run`을 실행합니다. `4x transition --to <phase> <id> && 4x run <id>`와 동일합니다.

`--to`를 생략하면 대상 단계는 `state.json`에 기록된 `role`로부터 **자동 감지**됩니다—feature가 `needs-attention`/`blocked`에 진입하기 전에 멈춰 있던 역할을 해당 작업 단계로 역매핑합니다 (예: `role: designer` → `designing`; `role: coder` → 라운드에 따라 `coding` 또는 `amending`). 자동 감지가 성공하면 실행 전에 `auto-detected target phase from role "<role>": <phase>`를 출력합니다. 역할을 매핑할 수 없으면(비어 있거나 알 수 없음) `accepting`으로 대체됩니다. `--to <phase>`를 명시적으로 지정하면 자동 감지보다 우선합니다.

```
4x retry F042-some-feature              # state.json의 role로부터 대상 단계 자동 감지
4x retry F042-some-feature --to amending
```

| 플래그 | 설명 |
|------|-------------|
| `--to <phase>` | 복구할 대상 단계 (기본값: `state.json`의 role로부터 자동 감지, 매핑할 수 없으면 `accepting`) |
| `--phase-override <phase>:<runner>:<model>` | 재실행되는 `4x run`으로 전달됩니다 (반복 가능) — `4x run`의 `--phase-override`와 동일한 형식과 의미 |

수동 `transition` / `retry --to <phase>`로 설정된 단계는 이후 `4x run` 복구 과정에서도 존중됩니다: `manualPhase` 플래그가 표시되어 `SmartResumePhase`가 디스크의 산출물로부터 유추한 이전 단계로 되돌리지 않습니다. 즉 `retry --to deep-reviewing`은 실제로 `deep-reviewing`에서 재개되며 `coding`으로 되돌아가지 않습니다.

상태를 변경하는 명령(`transition`, `retry`, `force-done`, `done`)은 `state.json`에 대해 단일 잠금 read-modify-write로 단계 변경을 수행하므로, 실행 중인 `4x run`이 쓰고 있는 feature에 대해 실행해도 서로의 업데이트를 덮어쓰지 않습니다. 기능별 잠금을 타임아웃 내에 획득하지 못하면 명령은 멈추지 않고 명확한 오류로 실패합니다.

기능이 현재 `needs-attention` 또는 `blocked` 상태가 아니면 오류가 발생합니다.

---

## `4x gate`

마이닝된 후보 feature에 F097 evolve **value gate** 거부 레이어를 적용합니다. 순수 CLI 결정론적 거부이며 LLM을 호출하지 않습니다. `gate` LLM 역할은 두 단계 사이에서 실행되며(evolve driver가 오케스트레이션), `gate-verdicts.json`을 출력합니다.

`--pre` 또는 `--post` 중 하나를 반드시 지정해야 합니다:

- `--pre` — PRE-거부: `.4x/candidates.json`을 읽고, 기존 feature 및 배치 내 중복과 Jaccard 유사한 후보를 제거하여 생존자를 `.4x/gate-input.json`에 기록합니다.
- `--post` — POST-거부: `.4x/gate-input.json` + `.4x/gate-verdicts.json`을 읽고, 재정의 불가능한 하드 거부(non-accept / `why_not_hack` 누락 / `value_floor` 미달 / 기존과 중복 / `max_accept_per_run` 초과 / `max_backlog_undone` 초과)를 적용하여 통과한 후보(`value_score`/`why_not_hack` 포함)를 `.4x/accepted-candidates.json`에 기록합니다.

임계값은 `settings.json`의 `evolution` 섹션(`value_floor`, `max_accept_per_run`, `max_backlog_undone`, `dedup_threshold`)에서 가져옵니다.

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

지속적 자기 개선 파이프라인을 1라운드 실행하여 기존 진화 부품들을 반복 실행 가능한 폐쇄 루프로 연결합니다:

**mine → gate (pre → gate LLM 역할 → post) → enrich → enqueue → (선택) auto-run 메타 루프 → learnings가 다음 라운드에 피드백.**

CLI 레이어는 직접 LLM을 호출하지 않습니다 — gate 역할과 enrichment 모두 `runner` 하위 프로세스로 실행됩니다. 각 호출은 정확히 **1라운드**를 실행합니다. 여러 라운드는 외부 구동(cron 또는 `4x evolve` 반복 호출)으로 수행합니다. 각 라운드의 결과는 `.4x/evolve-report.md`에 기록됩니다.

파이프라인 단계:

1. **mine** — `.4x/`를 스캔하여 실패 신호(에스컬레이션 / 정체된 feature / 반복 FAIL 패턴)를 탐지하고, 중복 제거 후 `.4x/candidates.json`에 병합합니다.
2. **gate pre** — Jaccard 중복 제거로 생존자를 `.4x/gate-input.json`에 기록합니다.
3. **gate role** — `gate` LLM 역할을 시작하여 `.4x/gate-verdicts.json`을 기록합니다.
4. **gate post** — 재정의 불가능한 거부 + 수렴 상한을 적용하여 `.4x/accepted-candidates.json`에 기록합니다.
5. **enrich + enqueue** — 통과한 각 후보를 `not-started` feature YAML로 구현합니다(enrichment 실패 시 후보 텍스트로 생성한 기본 feature로 폴백, `enriched=false` 표시).
6. **auto-run**(선택) — 대기열에 넣은 각 feature의 메타 루프를 실행합니다(F098 self-mod scope guard로 보호).

공회전 방지: 어떤 라운드에서 아무것도 수락하지 않으면 `.4x/evolve-state.json`의 `consecutiveNoAccept`가 증가합니다. `evolution.max_idle_rounds`(기본값 3, `<= 0`으로 비활성화)에 도달하면 다음 호출이 조기 중단되고 보고서를 `Halted`로 표시하며 exit 0으로 종료합니다. `--force`로 재정의할 수 있습니다.

```
4x evolve                        # 1라운드 실행, feature는 not-started 유지
4x evolve --dry-run              # 읽기 전용: mine/dedupe 요약 출력, 파일 기록 없음
4x evolve --auto-run             # 대기열에 넣은 feature의 메타 루프도 실행
4x evolve --force                # 공회전 방지 중단 우회
```

| 플래그 | 설명 |
|---|---|
| `--auto-run` | 대기열에 넣은 각 feature의 메타 루프 실행(F098 self-mod guard 항상 강제) |
| `--dry-run` | 읽기 전용 분석: mined/deduped 수 출력, 파일 기록·runner 시작·feature 생성 없음 |
| `--min-occurrences` | 실패 패턴이 후보가 되기 위한 distinct-feature 임계값(기본값 3) |
| `--force` | 공회전 방지 중단을 재정의하여 연속 유휴 라운드 후에도 실행 |
| `--runner` | gate / enrich / auto-run에 사용할 runner 플러그인(기본값 `evolution.gate_runner` 또는 프로젝트 기본값) |
| `--timeout` | LLM 하위 프로세스 타임아웃 초(기본값 3600) |
| `--max-rounds` | `--auto-run` 시 feature당 최대 라운드 수(기본값 5) |

Dashboard는 `GET /api/evolve-report`를 통해 최신 보고서를 표시합니다.

---

## `4x check <feature-id>`

상태 전환 없이 가드레일 검사를 실행합니다.

```
4x check <feature-id> [--json]
```

| 플래그 | 설명 |
|---|---|
| `--json` | 결과를 JSON으로 출력 |

검사 항목: 필수 파일, 베이스라인, 범위, 의존성, 백로그 드리프트. 통과 시 종료 코드 0, 실패 시 1.

---

## `4x doctor`

병합된 설정(`.4x/settings.json` + `~/.4x/settings.json`)과 워크스페이스 무결성에 대해 읽기 전용 헬스 체크를 실행합니다. 실행을 시작하기 전에 사용합니다. LLM을 호출하지 않으며 어떤 러너도 설치할 필요가 없습니다.

```
4x doctor [--json]
```

| 플래그 | 설명 |
|---|---|
| `--json` | 전체 보고서를 JSON으로 출력 (CI용) |

검사는 섹션별로 그룹화됩니다:

- **settings** — `settings.json` 로드 가능 여부, `project.name` 비어있지 않은지, 최소 하나의 러너가 정의되어 있는지, `default_runner`가 러너 맵에 존재하는지.
- **runners** — 각 러너의 `command`가 `PATH`에서 해석 가능한지 (없으면 FAIL이 아닌 WARN — 러너가 원격 머신에 있을 수 있으므로).
- **roles** — 기본 러너를 통해 각 역할(designer/coder/reviewer/tester/acceptor)이 사용할 실제 모델과 reviewer의 `deep_model`을 해석.
- **workspace** — 고아 worktree(기능이 done/abandoned인데 `.worktrees/4x/<id>`가 남아있음), 대응하는 기능 없는 worktree, 부실 상태(`active=true`인데 프로세스가 종료됨), 형식이 잘못된 기능 YAML.

각 줄에는 `PASS`, `WARN`, 또는 `FAIL` 접두사가 붙고, 요약 카운트가 표시됩니다.

종료 코드: FAIL이 없으면 `0` (WARN은 종료 코드에 영향 없음), 검사 하나라도 실패하면 `1`. `doctor`는 엄격히 읽기 전용입니다 — `state.json`을 다시 쓰거나 worktree를 정리하거나 설정을 수정하지 않습니다.

```bash
# CI 게이트: FAIL 검사가 있으면 빌드 실패
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

기능의 `test-strategy.yaml`에서 verify 명령어를 실행하고 결과를 `rounds/round-{N}/verify.json`에 기록합니다.

명령어는 `verify_groups`를 통해 그룹으로 구성할 수 있습니다: 그룹은 병렬로 실행되고, 그룹 내 명령어는 순차적으로 실행됩니다. 그룹 내 명령어가 실패하면 해당 그룹의 나머지 명령어는 건너뛰지만, 다른 그룹은 계속 실행됩니다. `verify_commands`만 정의된 경우 단일 순차 `default` 그룹으로 폴백합니다. 둘 다 선언하면 오류입니다.

병렬 실행은 전적으로 CLI가 처리합니다 — LLM은 관여하지 않습니다. Tester 역할은 verify 명령어를 직접 실행하는 대신 이 명령어를 호출합니다; 사람도 디버깅용으로 독립적으로 실행할 수 있습니다.

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| 플래그 | 설명 |
|---|---|
| `--round` | 라운드 번호 (기본값: state.json의 현재 라운드) |
| `--timeout` | 전체 그룹에 대한 타임아웃 (기본값: 5m) |
| `--json` | 전체 verify.json을 JSON으로 출력 |

건너뛰지 않은 모든 명령어가 통과하면 종료 코드 0, 하나라도 실패하면 1.

---

## `4x transition <feature-id>`

상태 전환을 강제합니다.

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| 플래그 | 설명 |
|---|---|
| `--to` | 대상 단계 (필수) |
| `--role` | 전환을 수행하는 역할 |
| `--json` | JSON 형식으로 출력 |

상태 머신에 따라 전환이 합법적인지 검증합니다. 상태가 없으면 자동 초기화합니다. `testing → accepting` 전환은 추가 게이트를 실행합니다 (verify.json, test-report.md, final-report.md가 존재해야 하며 검증을 통과해야 함).

`settings.json` 또는 기능 YAML에 `hooks`가 선언되어 있으면, `pre_{phase}` 훅은 전환 전에, `post_{phase}` 훅은 전환 후에 실행됩니다. `block` pre 훅이 실패하면 전환이 중단됩니다; `block` post 훅이 실패하면 기능이 `needs-attention`으로 이동합니다. 전체 설정 형식은 [단계 훅](concepts.md#단계-훅)을 참조하세요.

---

## `4x event <feature-id>`

`events.jsonl`에 이벤트를 추가합니다.

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| 플래그 | 설명 |
|---|---|
| `--type` | 이벤트 유형 (필수) |
| `--role` | 이벤트를 트리거한 역할 |
| `--round` | 라운드 번호 |
| `--action` | 액션 이름 |
| `--detail` | 추가 세부 텍스트 |

---

## `4x prompt <feature-id>`

기능에 대한 역할 프롬프트를 출력합니다.

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| 플래그 | 설명 |
|---|---|
| `--role` | 대상 역할 (생략 시 현재 상태에서 추론) |
| `--round` | 라운드 번호 |

로케일 주입(사용자 설정 또는 `LANG` 환경 변수), 계획 문서 자동 포함, 프로젝트/역할 인클루드를 지원합니다. spec/plan 문서는 공유 해석기(`protocol.ResolveDesignDoc`)를 통해 위치합니다 — 기능 YAML의 `spec`/`plan` 필드 우선, 그다음 `docs/design/{id}-{type}.md`, 그다음 `FNNN-` 접두사를 제거한 `docs/design/{slug}-{type}.md` 폴백 — 따라서 프롬프트는 대시보드 개요와 동일한 문서를 봅니다. [설계 문서 해석](concepts.md#설계-문서-해석)을 참조하세요.

`tester` 역할의 경우, 기능의 `test-strategy.yaml`에 나열된 `profiles`가 해석(`loadProfiles`)되어 `== Test Profile: {name} ==` 블록으로 프롬프트에 주입됩니다. 각 프로파일의 내용은 `settings.json` `test_profiles[name]`(`content` 또는 `include`)이 있으면 그것에서, 없으면 기본 제공 `templates/profiles/{name}.md`에서 가져옵니다. [테스트 프로파일](concepts.md#테스트-프로파일)을 참조하세요.

---

## `4x done <feature-id>`

pending-review 기능을 완료로 표시합니다. 기능에 worktree(`.worktrees/4x/<id>`)가 있으면 자동으로 브랜치를 main으로 병합하고 worktree와 브랜치를 제거합니다.

```
4x done <feature-id>
```

기능이 `pending-review` 단계에 있을 때만 작동합니다. 다른 단계에서는 오류가 발생합니다.

병합 충돌 또는 병합 오류가 발생하면 기능은 `pending-review` 상태로 유지되고 worktree가 보존되며 안내가 출력됩니다. 멀티 리포 모드에서는 충돌 리포 이름이 `repo: <name>`으로 표시됩니다. 충돌 해결 후 `4x merge <id>`를 사용하여 완료하세요.

병합 전에 4x는 메인 워크스페이스에 자신이 기록한 pipeline 상태(`.4x/features/*.yaml`, `.4x/learnings.json`, `.4x/learnings-context.md`)를 `chore(<feature-id>): 4x pipeline state`로 커밋합니다. 이 커밋은 지정된 경로만 대상으로 하므로, 메인 워크스페이스의 다른 커밋되지 않은 tracked 변경은 그대로 남고 여전히 병합을 중단시킵니다. `4x merge`도 완료 전에 동일하게 동작합니다.

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->

비터미널 단계에서 기능을 강제로 완료 처리합니다. 정상 파이프라인을 건너뛰는 이유를 문서화하기 위해 `--reason`이 필수입니다.

```
4x force-done <feature-id> --reason "코드 리뷰 완료 및 테스트 통과, e2e 테스트는 병합 후로 연기"
```

기능을 `pending-review`로 전환하고, 사유가 포함된 `force-done` 이벤트를 기록한 후 `4x done`과 동일한 병합 흐름을 실행합니다. `needs-attention`, `blocked` 또는 활성 단계 어디에서든 작동합니다.

대시보드는 이를 `POST /api/force-done`(`{id, reason}`)으로 노출합니다.

| 플래그 | 설명 |
|---|---|
| `--reason` | 기능을 강제 완료하는 이유 (필수) |
| `--json` | 결과를 JSON으로 출력 |

---

## `4x merge <feature-id>`

`4x done`에서 발생한 충돌을 해결한 후 병합을 완료합니다.

```
4x merge <feature-id>
```

기능이 `pending-review` 또는 `done` 단계이고 `.worktrees/4x/<id>`에 worktree가 존재할 때만 작동합니다. worktree에서 해결된 충돌을 커밋하고, main으로 병합한 다음, worktree와 브랜치를 제거합니다. 기능이 아직 `pending-review` 상태라면 병합 성공 후 `done`으로 표시됩니다.

멀티 리포 모드에서는 해결된 충돌이 리포별로 커밋(`.worktrees/4x/<id>/<repo-name>/` 아래 각 리포가 독립적으로 스테이징 및 커밋)된 후, 모든 리포가 전부 성공하거나 전부 실패하는 방식으로 병합됩니다. 충돌이 재발하면 충돌 리포 이름이 `repo: <name>`으로 표시됩니다.

---

## `4x clean [feature-id]`

완료된 기능의 워크스페이스 아티팩트(`logs/`, `rounds/`, 보고서, `state.json`, `events.jsonl`)를 제거하여 디스크 공간을 확보합니다. 기능 정의(`.4x/features/*.yaml`)와 기능 상태는 항상 보존됩니다.

```
4x clean              # 정리 가능한 기능 + 크기 나열, 확인 후 정리
4x clean --dry-run    # 나열만, 삭제 없음
4x clean --force      # 확인 프롬프트 건너뜀
4x clean <feature-id> # 단일 기능 정리 (done/abandoned이어야 함)
```

`done` 또는 `abandoned` 상태이고 워크스페이스 디렉토리가 존재하는 기능만 대상입니다. 활성(실행 중) 기능은 절대 정리되지 않으며, `blocked` / `needs-attention` 기능은 디버그 아티팩트가 유지되도록 보존됩니다. 정리는 상태 머신 전환이 아닙니다 — 기능 라이프사이클을 변경하지 않습니다.

---

## `4x learn`

`.4x/learnings.json`에 누적된 개발 교훈인 회고 학습 내역을 관리합니다.

각 기능의 Acceptor가 `retro-learnings.json`을 작성하면, CLI가 이를 `.4x/learnings.json`에 수집합니다. CLI는 각 역할의 프롬프트를 생성할 때 해당 역할의 카테고리로 `.4x/learnings.json`을 직접 필터링(active/candidate 분할 할당)하여 주입합니다 — Designer가 먼저 선택하는 중간 단계는 없습니다. 학습 내역은 전적으로 CLI가 관리하며 — 러너는 `learnings.json`을 직접 쓰지 않고, 학습 내역 처리 실패 시 경고만 출력하며 상태 전환을 차단하지 않습니다.

```
4x learn add --category <cat> --content <text>  # learning 수동 추가 (standalone session용)
4x learn add --category ops --content "..." --json  # JSON 출력: {"id":"L0xx","added":true}
4x learn list                     # active + candidate 학습 내역 나열 (기본값)
4x learn list --category=testing  # 카테고리 필터링
4x learn list --status=active     # 상태 필터링 (active, candidate, stale, promoted)
4x learn list --ineffective       # 비효과적 항목만 표시 (used≥3 + 30일 + 동일 카테고리 지속)
4x learn prune                    # 오래된 항목(90일 이상 미사용) 표시 및 삭제
4x learn prune --dry-run          # 삭제 없이 오래된 항목 미리보기
4x learn promote <id>             # 학습 내역을 promoted로 표시 (유지하되 더 이상 주입 안 함)
4x learn remove <id>              # 학습 내역 항목 삭제
```

`learn add`는 기존 항목과의 유사성 검사(정확 일치, 정규화, Jaccard 유사도)를 수행합니다. 퍼지 중복이 발견되면 기존 ID를 보고하고 쓰기하지 않습니다.

- 카테고리: `design`, `code-quality`, `testing`, `review`, `tooling`, `process`, `ops`
- 상태: `active`(주입 가능), `candidate`(새 harvest, 크로스 피처 검증 대기), `stale`(90일 이상 미사용, 읽을 때 자동 표시), `promoted`(템플릿/지침으로 승격됨)
- candidate 항목은 ID 뒤에 `*` 접미사가 표시됩니다. 다른 feature에서 독립적으로 생성되거나 Designer가 선택하면 자동으로 active로 승격됩니다
- 비효과적 항목은 `active!` 상태로 표시됩니다: 3회 이상 주입, 30일 이상 경과, 동일 카테고리의 새로운 learning이 계속 발생하는 경우
- 활성 항목 100개의 소프트 상한에 도달하면 `4x learn prune` 권장 경고가 표시됩니다 — 항목은 절대 자동 삭제되지 않습니다

---

## `4x mine`

`.4x/` 전체 이력을 스캔하여 실패 신호를 수집하고 `.4x/candidates.json`에 후보 풀로 집계합니다. 자동 발견(단일 실행의 딥 리뷰 PASS에서만 동작하며 `[NEW-FEATURE]` 마커를 파싱)과 달리, miner는 **모든** 기능에서 가장 풍부한 실패 데이터(에스컬레이션, 정체된 기능, 반복적 리뷰 실패)를 스캔합니다.

Miner는 순수 CLI/프로토콜 계층 스캔으로 — LLM을 호출하지 않으며 기능을 생성하지 않습니다. 후보만 생성하며, 후보가 실제 기능으로 승격될지는 나중에 F097 gate가 결정합니다.

```
4x mine                          # 스캔 후 .4x/candidates.json 기록
4x mine --dry-run                # 기록 없이 요약만 출력
4x mine --min-occurrences 5      # 실패 패턴 임계값 높이기 (기본값 3)
4x mine --output path.json       # 커스텀 경로에 기록
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--min-occurrences` | `3` | 반복 리뷰 이슈가 후보가 되기 위한 distinct-feature 수 |
| `--output` | `.4x/candidates.json` | 후보 풀 출력 경로 |
| `--dry-run` | `false` | 요약만 출력, 기록 없음 |

세 가지 스캐너가 풀에 데이터를 공급하며 각 후보에 추적성을 위한 `source`를 태그합니다:

- **escalation** — 각 라운드의 `escalation.json`(`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`)을 읽습니다
- **stuck** — `needs-attention` / `abandoned` / `blocked` 상태에 멈춘 기능으로, `state.json` 또는 최신 에스컬레이션에서 차단 사유를 추출합니다
- **fail-pattern** — `>= --min-occurrences`개의 distinct feature에 걸쳐 반복되는 리뷰/딥 리뷰 FAIL 이슈(Jaccard 유사도로 클러스터링); 각 클러스터는 리뷰 체크리스트 후보 학습도 생성합니다

스캔은 최선의 노력으로 수행됩니다: 손상된 기능 하나가 경고를 출력해도 나머지 스캔을 중단하지 않습니다. 후보는 기존 기능 YAML, 이전 `candidates.json`, 현재 배치 내에서 중복 제거(Jaccard)됩니다.

---

## `4x config`

사용자 수준 설정(`~/.4x/settings.json`)을 관리합니다.

```
4x config list          # 모든 사용자 설정 표시
4x config get <key>     # 값 가져오기
4x config set <key> <value>  # 값 설정
```

키는 점으로 구분된 경로입니다. 지원되는 형식:

| 키 | 예시 | 설명 |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | UI / 프롬프트 로케일 |
| `theme` | `4x config set theme dark` | 대시보드 테마 |
| `default_runner` | `4x config set default_runner claude` | 기본 러너 플러그인 |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | 러너별 `command`/`model`/`tty`/`stdin`/`quiet` |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | 역할별 `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` |

`role.deep-reviewer.parallel_reviewers`는 딥 리뷰가 팬아웃하는 병렬 sub-reviewer 수를 제어합니다(`1` = 단일 에이전트 폴백). `role.deep-reviewer.angles_per_reviewer`는 각 그룹의 관점 수를 고정합니다(비워두면 자동 균형). [개념 → 병렬 딥 리뷰](concepts.md)를 참조하세요.

---

## `4x sync`

기존 프로젝트에 내장된 플러그인 파일을 다시 배포합니다.

```
4x sync [--dry-run]
```

| 플래그 | 설명 |
|---|---|
| `--dry-run` | 파일을 쓰지 않고 차이점만 보고 |

각 파일을 created, updated, 또는 current로 보고합니다.

---

## `4x skills`

이 저장소의 `skills/` 디렉터리에 포함된 skill을 관리합니다. 설치는 **symlink 전용** — 4x는 `skills/<name>/`을 `~/.claude/skills/<name>`에 링크하므로, 이후 `git pull`을 하면 재설치 없이 skill이 자동으로 업데이트됩니다. 이 명령들은 4x 저장소 내부에서 실행하세요(`skills/` 디렉터리는 현재 디렉터리에서 위로 거슬러 올라가며 찾습니다).

```
4x skills list [--json]     # 사용 가능한 skill 목록 (이름 + 설명)
4x skills install <name>    # skills/<name>/을 ~/.claude/skills/<name>에 링크
4x skills remove <name>     # ~/.claude/skills/<name> symlink 제거
```

- `list`는 설치된 skill을 `✓`로 표시하고, owner-only skill(예: `4x-autopilot`)을 WARNING으로 표시합니다.
- `install`은 멱등적입니다 — 이미 링크된 skill을 다시 설치해도 아무 일도 일어나지 않습니다. 실제 디렉터리나 다른 곳을 가리키는 symlink 덮어쓰기는 거부합니다.
- `remove`는 symlink만 삭제합니다. 저장소 내부 파일은 절대 삭제하지 않으며, 실제(symlink이 아닌) 항목의 삭제는 거부합니다.

`4x-autopilot`을 설치하면 WARNING이 출력됩니다: 이는 owner-only(완전 자동 merge)입니다.

---

## `4x live [path...]`

4x Live 대시보드 서버를 시작합니다.

```
4x live [path...] [flags]
```

| 플래그 | 단축 | 기본값 | 설명 |
|---|---|---|---|
| `--port` | `-p` | `4567` | 서버 포트 |
| `--web` | `-w` | `false` | 브라우저에서 열기 |
| `--app` | `-a` | `false` | macOS 네이티브 앱 열기 |

경로 인수 없이 실행하면 `~/.4x/recent-projects.json`에서 최근 프로젝트를 로드합니다(LRU, 최대 20). 경로를 지정하면 각각을 프로젝트 탭으로 엽니다.

---

## `4x guard-tool`

<!-- alias: 4x guardtool -->
내부 PreToolUse 훅(숨김, 기계 전용). `claude` runner가 `reviewer`/`deep-reviewer` 역할에 주입합니다. 해당 라운드의 review-package.md가 존재하면 리뷰어가 직접 실행하는 `git diff`/`git log`/`git show` 호출이 review-package.md를 가리키는 메시지와 함께 부드럽게 거부됩니다. Claude Code 훅 JSON을 stdin에서, `FOURX_ROLE` / `FOURX_REVIEW_PACKAGE` 환경 변수를 읽습니다. 파싱 실패나 일치하지 않는 명령은 허용됩니다(exit 0). build/test/lint나 다른 역할을 막지 않으며 실행을 실패시키지 않습니다.

```
echo '{"tool_name":"Bash","tool_input":{"command":"git diff HEAD"}}' | FOURX_ROLE=reviewer FOURX_REVIEW_PACKAGE=/path/to/review-package.md 4x guard-tool
```

---

## `4x mcp`

Model Context Protocol (MCP) 서버를 시작합니다.

```
4x mcp
```

4x MCP stdio 서버를 시작하여 4x CLI 명령어를 LLM 클라이언트(예: Claude Code, Cursor)에 MCP 도구로 노출합니다.
