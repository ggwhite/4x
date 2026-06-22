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
| `--subtask` | `"id:name"` 또는 `"id:name:description"` 형식의 하위 작업 (반복 가능) |
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

루프는 다음을 수행합니다: init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review. 리뷰 실패 시 코드가 다시 수행됩니다. 테스트 실패 시 루프가 코딩으로 재진입합니다.

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

## `4x batch`

여러 기능에 대한 배치 작업입니다.

### `4x batch plan`

의존성 인식 실행 계획을 생성합니다.

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--dry-run` | `false` | 파일을 쓰지 않고 스케줄만 출력 |
| `--max-chain` | `4` | 클러스터당 최대 체인 길이 |

`.4x/batch-plan.json`에 기록합니다.

### `4x batch next`

다음 실행 가능한 기능을 표시합니다(계획과 현재 상태 기반).

```
4x batch next [--json]
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--json` | `false` | 하위 작업 프런티어를 포함한 JSON 형식으로 출력 |

`--json` 없이는 기능 ID를 일반 텍스트로 출력합니다(하위 호환). `--json`과 함께 사용하면 `subtaskFrontier`(모든 의존성이 완료된 하위 작업)를 포함하는 JSON 객체를 출력합니다. 적격 기능이 없으면 JSON 모드에서 `null`을 반환합니다.

### `4x batch run`

적격 기능을 의존성 순서대로 순차적으로 실행합니다.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--runner` | 설정 기본값 | 러너 플러그인 이름 |
| `--max-rounds` | `5` | 기능당 최대 라운드 수 |
| `--timeout` | `3600` | 단계별 타임아웃 (초) |
| `--no-auto-merge` | `false` | 완료된 기능을 자동 병합하지 않고 `pending-review`에 유지 |

기능 간에 `.4x/batch-stop` 파일을 폴링하여 정상 종료합니다.

실행이 종료되면(정상, 중지, 인터럽트(`SIGTERM`/`SIGINT`) 또는 크래시) `.4x/batch-report.json`을 기록하여 실행을 요약합니다(`outcome`, completed/failed/remaining 카운트, 러너, 소요 시간, 기능별 최종 상태). [배치 모드 → 실행 보고서](batch.md#run-report)를 참조하세요.

기본적으로, 기능이 완료(`pending-review` 도달)되면 배치가 자동으로 worktree 브랜치를 main에 병합하여 다음 기능이 업데이트된 main에서 분기하도록 합니다 — 무인 연속 배치가 가능해집니다. 병합 충돌 시 배치가 정상적으로 일시정지하며, 기능을 `pending-review`에 worktree를 보존한 상태로 두고, [대시보드](dashboard.md)가 충돌을 표시할 수 있도록 `.4x/batch-conflict.json` 신호 파일(기능, 충돌 리포, 파일)을 기록합니다. 충돌을 해결하고 `4x merge <id>`를 실행한 다음 `4x batch run`을 다시 실행하여 계속하세요. 충돌 신호는 각 실행 시작 시 지워집니다. 비 충돌 병합 오류는 경고를 출력하고 배치가 다음 기능으로 계속됩니다. `--no-auto-merge`를 전달하면 이전 동작(기능이 `pending-review`에서 수동 리뷰를 위해 대기)으로 복원됩니다.

설정에 `isolation: "worktree"`가 설정된 경우 각 기능이 자체 격리된 worktree에서 실행됩니다. 멀티 리포 모드에서는 각 기능이 복합 worktree(`.worktrees/4x/<feature-id>/`)를 리포별 하위 디렉토리와 함께 갖고, 커밋은 라운드별로 수행됩니다(완료까지 지연되지 않음). Hub 리포지토리(`hub_repos` 설정 또는 `workspace.repos[*].hub: true`)는 공유 리포지토리 클러스터링에서 제외되어 병렬 실행이 가능합니다.

### `4x batch stop`

현재 기능 완료 후 실행 중인 배치에 종료 신호를 보냅니다.

```
4x batch stop
```

`.4x/batch-stop` 시그널 파일을 생성합니다.

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

## `4x mcp`

Model Context Protocol (MCP) 서버를 시작합니다.

```
4x mcp
```

4x MCP stdio 서버를 시작하여 4x CLI 명령어를 LLM 클라이언트(예: Claude Code, Cursor)에 MCP 도구로 노출합니다.
