# CLI 레퍼런스

모든 feature-id 인수는 대소문자 구분 없는 접두사 매칭을 지원합니다. `4x run f001`, `4x run F001-user`, `4x run F001` 모두 `F001-user-authentication-w`로 해석됩니다. 모호한 접두사는 일치 항목을 나열하는 오류를 생성합니다.

---

## `4x init`

현재 디렉토리에 `.4x/` 워크스페이스를 초기화합니다.

```
4x init
```

- 프로젝트 언어와 빌드/테스트/린트 명령어를 자동 감지
- 4개의 기본 러너(claude, codex, gemini, agy)로 `.4x/settings.json` 생성
- `.4x/plugins/`에 내장된 플러그인 파일 배포
- 루트 레벨 파일에 `@import` 라인 추가 (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md)
- `.4x/`가 이미 존재하면 오류 발생

---

## `4x new <title>`

새 기능을 생성합니다.

```
4x new "Feature title" [--repo <repo>...]
```

| 플래그 | 설명 |
|---|---|
| `--repo` | 범위에 포함할 리포지토리 (멀티 리포 기능에서 반복 사용 가능) |

`.4x/features/F{NNN}-{slug}.yaml`을 `not-started` 상태로 생성합니다.

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

루프는 다음을 수행합니다: init → designing → coding → reviewing → testing → accepting → pending-review. 리뷰 실패 시 코드가 다시 수행됩니다. 테스트 실패 시 루프가 코딩으로 재진입합니다.

의존성 게이트를 자동 확인합니다 — 의존 기능이 완료되지 않으면 차단됩니다.

설정에 `isolation: "worktree"`가 설정된 경우 `.worktrees/4x/<feature-id>/` 아래의 git worktree에서 실행됩니다.

---

## `4x status [feature-id]`

기능 상태를 표시합니다.

```
4x status              # 모든 기능, 상태별 그룹화
4x status <feature-id> # 단일 기능 상세 정보 및 하위 작업
4x status --pending    # pending-review 기능 필터링
```

그룹: Running, Review, Pending, Todo, Done (done은 최대 5개 표시). 백로그 드리프트 경고를 포함합니다.

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

## `4x transition <feature-id>`

상태 전환을 강제합니다.

```
4x transition <feature-id> --to <phase> [--role <role>]
```

| 플래그 | 설명 |
|---|---|
| `--to` | 대상 단계 (필수) |
| `--role` | 전환을 수행하는 역할 |

상태 머신에 따라 전환이 합법적인지 검증합니다. 상태가 없으면 자동 초기화합니다. `testing → accepting` 전환은 추가 게이트를 실행합니다 (verify.json, test-report.md, final-report.md, commit-plan.md가 존재해야 하며 검증을 통과해야 함).

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

로케일 주입(사용자 설정 또는 `LANG` 환경 변수), 계획 문서 자동 포함(`docs/design/{id}-spec.md` 및 `{id}-plan.md`), 프로젝트/역할 인클루드를 지원합니다.

---

## `4x done <feature-id>`

pending-review 기능을 완료로 표시합니다.

```
4x done <feature-id>
```

기능이 `pending-review` 단계에 있을 때만 작동합니다. 다른 단계에서는 오류가 발생합니다.

---

## `4x config`

사용자 수준 설정(`~/.4x/settings.json`)을 관리합니다.

```
4x config list          # 모든 사용자 설정 표시
4x config get <key>     # 값 가져오기
4x config set <key> <value>  # 값 설정
```

현재 지원되는 키: `locale`.

---

## `4x upgrade`

기존 프로젝트에 내장된 플러그인 파일을 다시 배포합니다.

```
4x upgrade [--dry-run]
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
4x batch next
```

### `4x batch run`

적격 기능을 의존성 순서대로 순차적으로 실행합니다.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--runner` | 설정 기본값 | 러너 플러그인 이름 |
| `--max-rounds` | `5` | 기능당 최대 라운드 수 |
| `--timeout` | `3600` | 단계별 타임아웃 (초) |

기능 간에 `.4x/batch-stop` 파일을 폴링하여 정상 종료합니다.

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
