# 설정

## 프로젝트 설정 (`.4x/settings.json`)

`4x init`으로 생성됩니다. 프로젝트 메타데이터, 러너 정의, 역할 모델 매핑을 포함합니다.

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
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus",
      "tty": true
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
  "default": "claude",
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
| `docs` | Designer 참조용 문서 파일 경로 |
| `rules` | 역할 프롬프트에 주입되는 프로젝트별 규칙 |

### 러너 설정

| 필드 | 설명 |
|---|---|
| `command` | 실행 파일 이름 |
| `args` | 인수. `{prompt}`와 `{promptFile}`은 런타임에 대체됩니다. `{model}`은 역할의 모델로 대체됩니다. |
| `model` | 이 러너의 기본 모델 |
| `tty` | 출력 캡처를 위한 PTY 사용 (Claude Code처럼 ANSI 출력이 있는 CLI 도구에 필요) |
| `stdin` | 인수 대신 stdin으로 프롬프트 전송 (Codex에서 사용) |

`args`에 `{model}`이 없으면 러너가 자동으로 `--model <model>`을 추가합니다.

### 역할 설정

| 필드 | 설명 |
|---|---|
| `model` | 이 역할의 모델 이름 |
| `deep_model` | 적대적 리뷰 패스용 모델 (reviewer 전용) |
| `instructions` | 역할 프롬프트에 주입되는 추가 지침 |
| `includes` | 역할 프롬프트에 포함할 파일 |

### 기타 설정 필드

| 필드 | 설명 |
|---|---|
| `hub_repos` | 공유 리포지토리 (배치 DAG 그룹화용) |
| `isolation` | `"worktree"`로 설정하면 기능을 git worktree에서 실행 |
| `max_concurrent_runs` | 대시보드 서버를 통한 최대 동시 실행 수 |
| `commit` | 커밋 전략: `"per-round"` (기본값), `"on-done"`, 또는 `"never"` |

## 사용자 설정 (`~/.4x/settings.json`)

전역 사용자 기본 설정. `4x config`로 관리합니다.

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

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
