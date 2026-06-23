# 설정

## 프로젝트 설정 (`.4x/settings.json`)

`4x init`으로 생성됩니다. 프로젝트 메타데이터, 러너 정의, 역할 모델 매핑을 포함합니다.

**4x Live 대시보드**에서 시각적으로 편집할 수도 있습니다 — "4x Live" 제목 옆의 톱니바퀴 아이콘(gear)을 클릭하거나 `Cmd+Shift+,`를 누르세요. 편집기는 폼 뷰와 raw JSON 뷰를 모두 지원하며, 필수 필드를 검증하고, 기록 전에 이전 설정을 `settings.json.bak`에 백업합니다.

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
	    "claude": {
	      "command": "claude",
	      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
	      "model": "opus",
	      "output_format": "stream-json"
	    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default_runner": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project 섹션

| 필드 | 설명 |
|---|---|
| `name` | 프로젝트 이름 (디렉토리에서 자동 감지) |
| `language` | 감지된 언어 |
| `build` | 빌드 명령어 |
| `test` | 테스트 명령어 |
| `lint` | 린트 명령어 |
| `setup` | 설정 명령어 (예: `docker-compose up -d`) |
| `description` | 프로젝트 설명 (선택 사항) |
| `docs` | Designer 참조용 문서 파일 경로 |
| `rules` | 역할 프롬프트에 주입되는 프로젝트별 규칙 |
| `includes` | 역할 프롬프트에 포함할 파일 |

### 러너 설정

| 필드 | 설명 |
|---|---|
| `command` | 실행 파일 이름 |
| `args` | 인수. `{prompt}`와 `{promptFile}`은 런타임에 대체됩니다. `{model}`은 역할의 모델로 대체됩니다. |
| `model` | 이 러너의 기본 모델 |
| `tiers` | 티어 이름을 러너 전용 모델 이름으로 매핑 (예: `{"opus": "claude-opus-4-5-20250514"}`). 조회 순서: 역할 model → tiers 변환 → 원래 이름으로 폴백. |
| `output_format` | `"stream-json"`으로 설정하면 runner stdout을 읽기 쉬운 `.log`와 raw `.stream.jsonl`로 파싱합니다. |
| `tty` | 출력 캡처를 위한 PTY 사용. `output_format`이 `"stream-json"`이면 무시됩니다. |
| `stdin` | 인수 대신 stdin으로 프롬프트 전송 (Codex에서 사용) |
| `quiet` | 러너의 터미널 stdout 출력을 억제합니다. 출력은 로그 파일에 기록됩니다. |

`args`에 `{model}`이 없으면 러너가 자동으로 `--model <model>`을 추가합니다.

### 역할 설정

| 필드 | 설명 |
|---|---|
| `model` | 이 역할의 모델 이름 |
| `deep_model` | 적대적 리뷰 패스용 모델 (reviewer 전용). **`deep-reviewing` 단계 실행에 필수** — 미설정 시 해당 단계가 건너뛰어지며 `testing`에서 바로 `accepting`으로 전환됩니다. |
| `max_fix_rounds` | `deep-reviewing` 단계에서 최대 자가 치유 반복 횟수 (`deep-reviewer` 전용; 기본값 2). 각 반복은 범위가 한정된 mini-coder + re-verifier를 실행하며, 상한 초과 시 `needs-attention`으로 에스컬레이션합니다. |
| `instructions` | 역할 프롬프트에 주입되는 추가 지침 |
| `includes` | 역할 프롬프트에 포함할 파일 |
| `screenshot_dir` | tester 스크린샷을 위한 디렉토리 경로 |
| `parallel_reviewers` | 딥 리뷰의 병렬 sub-reviewer 수 (deep-reviewer 전용; <=1이면 단일 에이전트 모드로 폴백) |
| `angles_per_reviewer` | sub-reviewer당 리뷰 관점 수 (deep-reviewer 전용; 0이면 자동 균등 분배) |

### 기타 설정 필드

| 필드 | 설명 |
|---|---|
| `hub_repos` | 공유 리포지토리 (배치 DAG 그룹화용) |
| `isolation` | `"worktree"`로 설정하면 기능을 git worktree에서 실행 |
| `max_concurrent_runs` | 대시보드 서버를 통한 최대 동시 실행 수 |
| `commit` | 커밋 전략: `"per-round"` (기본값), `"on-done"`, 또는 `"never"` |
| `profiles` | 명명된 파이프라인 프로파일 (역할 하위 집합); [프로파일](#프로파일) 참조 |
| `parallel_review_test` | reviewing 단계에서 reviewer와 tester를 동시 실행 (기본값 `false`) |
| `auto_discover_features` | 딥 리뷰 보고서의 `[NEW-FEATURE]` 마커에서 기능 자동 생성 (기본값 `false`); [자동 발견 기능](#자동-발견-기능) 참조 |
| `workspace` | 멀티 리포 워크스페이스 설정 (리포 이름 → 경로 매핑) |
| `hooks` | 라이프사이클 훅 (훅 포인트별 키, 예: post-run) |
| `health_check` | 글로벌 사전 테스트 환경 검사 명령어 (test-strategy.yaml에서 기능별 재정의 가능) |
| `test_profiles` | 커스텀 또는 재정의된 테스트 프로파일 정의 (프로파일 이름별 키) |
| `max_discovered_features` | 실행당 자동 생성되는 최대 기능 수; 미설정이거나 `<= 0`이면 기본값(`3`) 적용 |

### 자동 발견 기능

`auto_discover_features`가 `true`이면, 실행 루프가 최종 딥 리뷰 보고서(`deep-review-report.md`)를 **통과** 후 파싱하여 각 `[NEW-FEATURE]` 마커를 새 기능 YAML로 변환합니다 — 딥 리뷰어가 발견한 범위 밖 이슈를 묻히지 않고 포착합니다.

- **트리거 시점**: 최종 딥 리뷰가 통과할 때만 실행됩니다(첫 번째 PASS 또는 자가 치유 후 PASS). 중간 라운드, reviewer/tester 실패, 딥 리뷰 FAIL/needs-attention 경로에서는 실행되지 않습니다.
- **중복 제거**: 각 후보를 모든 기존 기능의 이름 + 설명, 그리고 같은 배치에서 이미 유지된 후보와 토큰 유사도로 비교합니다. 유사한 후보는 건너뜁니다.
- **수량 제한**: 실행당 최대 `max_discovered_features`(기본값 `3`)개의 기능이 생성됩니다; 나머지는 제한됨으로 기록됩니다.
- **출력**: `.4x/<feature-id>/` 아래에 생성됨 / 중복으로 건너뜀 / 제한됨 후보를 나열하는 `discovered-features.md` 요약이 기록되고, 생성된 기능마다 `feature-discovered` 이벤트가 추가됩니다.

이 모든 것은 CLI 계층(일반 텍스트 파싱 + 파일 쓰기, LLM 호출 없음)에서 발생하며 `accepting`으로의 전환을 차단하지 않습니다 — 오류는 최선의 노력으로 기록됩니다.

### 프로파일

프로파일은 기능에 대해 어떤 phase가 실행될지 선택하여, 단순한 기능이 전체 파이프라인을 건너뛸 수 있게 합니다. 나열되지 않은 phase는 통과됩니다 — 러너를 호출하거나 아티팩트를 확인하거나 가드를 실행하지 않고 합법적 경로를 따라 상태가 진행됩니다. `coding`만이 유일한 필수 phase입니다; 누락된 프로파일은 설정 오류입니다. 선택적 `design-reviewing` phase는 포함된 경우에만 실행되며, coding 시작 전에 `design-review-report.md`가 PASS해야 합니다.

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

각 phase 항목은 선택적 `runner` 및 `model` 재정의를 지원합니다:

| 필드 | 설명 |
|---|---|
| `phase` | Phase 이름 (선택 가능한 phase이어야 함: designing, design-reviewing, coding, reviewing, testing, deep-reviewing, accepting) |
| `runner` | 이 phase의 선택적 runner 재정의 |
| `model` | 이 phase의 선택적 모델 티어 재정의 |

**선택 우선순위:**

1. `4x run --profile <name>` — 명시적 재정의 (`profiles`에서 조회 후 기본 제공으로 폴백).
2. 그 외, `profiles` 섹션이 존재하면 기능의 `priority`에 따라 자동 선택: `null`/`0`/`1` → `full`, `2` → `normal`, `>=3` → `quick`.
3. `profiles` 섹션이 없으면 모든 기능이 `full`로 실행됩니다(우선순위 기반 자동 선택 비활성화 — 하위 호환).

세 개의 기본 제공 프로파일(`full`/`normal`/`quick`)은 `profiles` 섹션 없이도 항상 폴백으로 사용 가능합니다. 활성 프로파일 이름은 기능 상태에 기록되며 대시보드 카드에 표시됩니다.

`parallel_review_test`가 `true`이고 활성 프로파일이 `reviewer`와 `tester`를 모두 활성화하면, 두 읽기 전용 역할이 reviewing 단계에서 같은 worktree에서 동시 실행됩니다; 둘 다 통과하면 딥 리뷰로 진행하고, 그렇지 않으면 루프가 coding으로 재진입합니다.

## 사용자 설정 (`~/.4x/settings.json`)

전역 사용자 기본 설정과 러너 기본값. 크로스 프로젝트 설정으로 `4x config` 또는 대시보드의 **전역 설정** 편집기(사이드바 gear-G 버튼)를 통해 관리합니다.

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### 사용자 설정 필드

| 필드 | 설명 |
|---|---|
| `locale` | 역할 프롬프트 지침의 언어 |
| `theme` | 대시보드 테마 (`dark`/`light`) |
| `default_runner` | 기본 러너 이름 (프로젝트 설정으로 재정의됨) |
| `runners` | 러너 정의 (command, args, tty 등) |
| `roles` | 역할 모델 기본값 |
| `logLevel` | 최소 로그 레벨 (debug/info/warn/error; 기본값 "info"; FOURX_LOG_LEVEL 환경 변수로 재정의) |
| `logRetainDays` | ~/.4x/logs/ 로그 파일 보존 일수 (기본값 7) |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args`는 배열 필드입니다 — 설정하려면 `~/.4x/settings.json`을 직접 편집하세요.

### 로케일

역할 프롬프트 지침의 언어를 설정합니다. 지원되는 값:

| 값 | 언어 |
|---|---|
| `en` | 영어 (기본값) |
| `zh-TW` | 번체 중국어 |
| `zh-CN` | 간체 중국어 |
| `ja` | 일본어 |
| `ko` | 한국어 |
| `es` | 스페인어 |
| `fr` | 프랑스어 |
| `de` | 독일어 |
| `pt` | 포르투갈어 |
| `ru` | 러시아어 |
| `vi` | 베트남어 |

명시적으로 설정하지 않으면 `LANG` 환경 변수에서 로케일을 추론합니다.

## 설정 병합

`4x run` 또는 `4x prompt` 실행 시, 사용자 수준과 프로젝트 수준 설정이 딥 머지됩니다:

- **우선순위:** 프로젝트 > 사용자 > 기본값
- **러너 병합:** 필드 단위 — 프로젝트의 비-zero 필드가 사용자의 것을 재정의합니다. `args`는 전체 대체입니다(추가 아님). `tiers`는 키 수준에서 병합됩니다.
- **역할 병합:** 필드 단위 — 러너와 동일.
- **프로젝트 전용 필드**: `default_runner`, `runners`, `roles`를 제외한 모든 필드는 프로젝트 전용이며 사용자 설정으로 재정의되지 않습니다.

대시보드의 프로젝트 설정 편집기는 병합된 결과가 아닌 **원본** 프로젝트 설정을 표시합니다. 병합 후 최종 유효 설정을 보려면 프로젝트 설정의 **Merged** 탭을 사용하세요.
