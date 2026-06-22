# 시작하기

## 설치

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

Go 1.26+ 필요.

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 바이너리 다운로드

macOS, Linux, Windows (amd64 / arm64) 사전 빌드된 바이너리는 [Releases](https://github.com/ggwhite/4x/releases) 페이지에서 다운로드할 수 있습니다.

### 확인

다음으로 확인:

```bash
4x --help
```

## 프로젝트 초기화

```bash
cd my-project
4x init
```

이렇게 하면 다음을 포함하는 `.4x/` 디렉토리가 생성됩니다:
- `settings.json` — 프로젝트 설정, 러너(runner) 정의, 역할 모델 매핑
- `plugins/` — 러너 지침 파일 (SKILL.md, AGENTS.md 등)
- 루트 레벨 임포트 파일 (CLAUDE.md, AGENTS.md, GEMINI.md 등)

4x는 프로젝트 언어(Go, TypeScript, Java, Rust, Python)를 자동 감지하고 빌드/테스트/린트 명령어를 미리 채웁니다.

`.4x/`가 이미 존재하면 `init`은 오류와 함께 종료됩니다 — 플러그인 파일을 갱신하려면 `4x sync`를 사용하세요.

## 기능 생성

```bash
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

기능은 `.4x/features/{id}.yaml`에 저장됩니다. ID 형식은 `F{NNN}-{slug}`(slug 최대 23자)입니다.

`--repo`를 사용하여 범위에 포함할 리포지토리를 선언합니다(멀티 리포 프로젝트용).

## 루프 실행

```bash
# 기본 러너로 실행 (보통 claude)
4x run F001

# 러너 지정
4x run F001 --runner claude

# 반복 횟수 제한
4x run F001 --max-rounds 3

# 타임아웃 설정 (초)
4x run F001 --timeout 7200

# LLM 호출 없이 프롬프트만 미리보기
4x run F001 --dry-run
```

기능 ID는 접두사 매칭을 지원합니다 — `4x run F001`과 `4x run f001` 모두 작동합니다.

루프는 다음 순서로 실행됩니다: **Design → Code → Review → Test → Accept → Pending Review**. Review에서 문제가 발견되면 Code가 다시 실행됩니다. Test가 실패하면 루프가 반복됩니다(`--max-rounds`까지).

## 상태 확인

```bash
# 모든 기능
4x status

# 단일 기능 상세 정보
4x status F001

# pending review 필터링
4x status --pending
```

## 기능 완료

루프가 완료되면 기능은 `pending-review` 상태가 됩니다 — 사람의 승인을 기다립니다.

```bash
# 산출물 확인
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# 완료 표시
4x done F001
```

## 플러그인 파일 업그레이드

`4x` 바이너리를 업데이트한 후 내장된 플러그인을 다시 배포합니다:

```bash
4x sync            # 새 파일 배포
4x sync --dry-run  # 변경사항만 미리보기
```

## 다음 단계

- [CLI 레퍼런스](cli.md) — 모든 명령어와 플래그
- [핵심 개념](concepts.md) — 역할, 상태 머신, 프로토콜 이해
- [설정](configuration.md) — 모델, 러너, 로케일 맞춤 설정
