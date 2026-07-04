# 러너 & 플러그인

## 러너란?

러너(runner)는 4x CLI와 AI 도구 사이의 브릿지입니다. CLI가 역할 프롬프트를 생성하고 상태를 관리하며, 러너가 프롬프트를 AI에 전송하고 출력을 캡처합니다.

러너는 `.4x/settings.json`의 `runners` 키 아래에 설정됩니다. CLI는 러너를 서브프로세스로 호출합니다.

## 내장 러너

| 러너 | AI 도구 | 모드 | 상태 |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | 사용 가능 |
| `codex` | OpenAI Codex CLI | Stdin | 사용 가능 |
| `gemini` | Google Gemini CLI | Argument | 사용 가능 |
| `agy` | Antigravity CLI | Argument | 사용 가능 |
| `opencode` | OpenCode CLI | Argument | 사용 가능 |
| `copilot` | GitHub Copilot CLI | Argument | 사용 가능 (수동 설정) |
| `cursor` | Cursor IDE | Rules 파일 | 사용 가능 (수동 설정) |

`4x init`은 기본적으로 claude, codex, gemini, agy, opencode를 설정합니다. Copilot과 cursor는 `settings.json`에 수동으로 추가해야 합니다.

## 플러그인 파일

각 러너에는 `4x` 바이너리에 내장된 지침 파일이 있습니다. `4x init`이 `.4x/plugins/`에 배포하고 루트 레벨 파일에 임포트 라인을 추가합니다:

| 러너 | 플러그인 파일 | 루트 임포트 |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| opencode | `AGENTS.md` | AGENTS.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

또한 공유 지시 파일이 모든 러너용으로 `.4x/plugins/shared/`에 배포됩니다:

| 파일 | 용도 |
|---|---|
| `shared/CREATOR.md` | Feature Creator 흐름 — AI가 `4x new`로 기능을 생성하도록 안내 |

바이너리를 업데이트한 후 `4x sync`를 사용하여 플러그인 파일을 다시 배포하세요.

## 러너 실행 모델

```
4x run F001 --runner claude
    │
    ├── 현재 역할에 대한 프롬프트 생성
    ├── 프롬프트와 함께 러너 서브프로세스 호출
    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
    ├── 출력을 .4x/run/F001/logs/round-N-role.log에 캡처
    ├── 출력 아티팩트 확인
    └── 상태 전환 후 반복
```

### 종료 코드

| 코드 | 의미 | 동작 |
|---|---|---|
| 0 | 성공 | 다음 단계로 진행 |
| 1 | 소프트 실패 | 기능이 `blocked`로 이동 |
| 2 | 하드 오류 | 루프 중단, 주의 필요 |
| timeout | 제한 시간 내 응답 없음 | 소프트 실패로 처리 |

### 플레이스홀더 해석

러너 `args`에는 CLI가 서브프로세스를 실행하기 전에 치환하는 플레이스홀더를 포함할 수 있습니다:

| 플레이스홀더 | 치환 내용 |
|---|---|
| `{prompt}` | 역할 프롬프트 텍스트, 인수로 인라인 삽입 |
| `{promptFile}` | 프롬프트가 담긴 임시 파일 경로 |
| `{model}` | 이 역할에 해석된 모델 오버라이드 |

플레이스홀더 해석은 리터럴 플레이스홀더를 AI CLI에 전달하는 대신 **큰 소리로 실패**합니다:

- `{model}`이 있지만 모델 오버라이드가 해석되지 않음 → 러너가 `model not resolved for runner <name>` 오류를 반환합니다(`--model {model}`을 전달하면 CLI가 불명확한 오류로 거부).
- `{promptFile}`이 있지만 임시 파일을 생성하거나 쓸 수 없음(예: `/tmp` 꽉 참) → 러너가 래핑된 오류(`runner <name>: create prompt temp file: ...`)를 반환하고 부분적으로 생성된 임시 파일을 제거합니다.

해석 중 생성된 임시 파일은 이후 단계가 실패하더라도 항상 정리됩니다.

### Stream JSON 모드

`output_format: "stream-json"` 러너는 dashboard가 tail하는 읽기 쉬운 `.log`와 디버깅용 raw `.stream.jsonl` 두 파일을 씁니다. Claude Code는 기본적으로 이 모드를 사용합니다. `.log`의 tool-use 요약(예: Bash 명령어)은 고정된 길이에서 잘리지만, 자르는 지점은 UTF-8 문자 경계에 맞춰져 있어 멀티바이트 문자가 중간에 잘리지 않습니다.

### 비-PTY 프로세스 그룹 처리

비-PTY 러너(stream-json 모드, stdin 모드, 일반 인수 모드)는 독립적인 프로세스 그룹(`Unix`의 `Setpgid`)을 사용합니다. 실행 컨텍스트가 취소되면 프로세스 그룹에 즉시 `SIGKILL`이 전송됩니다 — SIGTERM 유예 기간이 없습니다. Windows에서는 기본 `exec.CommandContext` 동작이 적용됩니다.

### PTY 모드

`tty: true`인 러너(그리고 `output_format: "stream-json"`을 사용하지 않는)는 ANSI 이스케이프 시퀀스를 포함한 전체 출력을 캡처하기 위해 의사 터미널을 사용합니다. 상태 유지 ANSI 스트리퍼가 로그 파일을 정리합니다. PTY 경로는 정상 종료를 위한 전용 컨텍스트 감시자를 가진 `exec.Command`를 사용하며, 비-PTY 러너는 프로세스 그룹 수준의 취소를 가진 `exec.CommandContext`를 사용합니다(위 참조).

PTY 자식은 자체 세션/프로세스 그룹에서 실행됩니다. 실행 컨텍스트가 취소되면(예: 타임아웃 또는 Ctrl+C), 전체 프로세스 그룹에 `SIGTERM`이 전송되고, 5초 이내에 종료되지 않으면 `SIGKILL`로 에스컬레이션됩니다 — 실행이 끝난 후에도 자식 프로세스가 고아로 남지 않습니다.

### Stdin 모드

`stdin: true`인 러너(Codex)는 커맨드 라인 인수 대신 표준 입력으로 프롬프트를 받습니다.

## 역할별 다른 모델 사용

`.4x/settings.json`에서 설정합니다:

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

> **참고:** `deep_model`은 **reviewer** 역할에 설정합니다 (deep-reviewer가 아님). `roles.reviewer.deep_model`이 설정되지 않으면 `deep-reviewing` 단계가 **완전히 건너뛰어지며**, `testing`에서 바로 `accepting`으로 전환됩니다. 이는 설계 의도입니다: 딥 리뷰는 opt-in 기능입니다.

러너를 혼합할 수도 있습니다 — Design에는 Claude, Code에는 Gemini를 사용하는 식으로 — 각 단계를 다른 `--runner` 플래그로 수동 실행하고 단계 사이에 `4x transition`을 사용합니다.

## 플러그인 작성

플러그인은 간단한 계약을 따릅니다 — `.4x/` 파일을 읽고, AI 작업을 수행하고, 결과를 다시 씁니다:

1. `.4x/features/{id}.yaml`을 읽어 기능을 파악
2. `state.json`을 읽어 현재 단계를 파악
3. 단계별 입력 읽기 (task-brief.md, scope 등)
4. 작업 수행 (LLM 호출, 도구 실행)
5. 단계별 출력 작성 (coder-report.md, review-report.md 등)
6. 적절한 코드로 종료 (0 = 성공, 1 = 소프트 실패, 2 = 하드 오류)

SDK 불필요. 런타임 의존성 없음. 파일만 있으면 됩니다.
