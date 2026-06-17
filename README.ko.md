[English](README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | **한국어** | [Español](README.es.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

<p align="center">
  <img src="docs/assets/4x-banner.svg" alt="4X — Design. Code. Review. Test." width="480">
</p>

<p align="center">
  <img src="docs/assets/demo.gif" alt="4x demo" width="720">
</p>

**4x는 소프트웨어 엔지니어링 루프를 네 가지 전문 단계로 나누는 다중 역할 AI 개발 프레임워크입니다** — Design, Code, Review, Test — 각 단계는 전담 AI 에이전트가 수행합니다. 4X 전략 게임(eXplore, eXpand, eXploit, eXterminate)처럼, 이름은 고유한 강점을 가진 역할들이 복잡성을 정복하기 위해 협력하는 시스템을 나타냅니다.

---

## 왜 4x인가?

단일 에이전트 코딩은 빠르지만 취약합니다. 하나의 AI에게 설계, 구현, 리뷰, 테스트를 모두 맡기면 — 같은 호흡 속에서, 같은 편향으로 진행됩니다. 작은 작업에는 괜찮지만 실제 기능 개발에서는 무너집니다.

4x는 루프를 분리합니다. 각 역할은 집중된 작업, 제한된 범위, 그리고 다른 역할의 추론에 접근할 수 없습니다. Designer는 코드를 작성하지 않습니다. Coder는 자신의 작업을 스스로 판단하지 않습니다. Reviewer는 설계부터 적대적(adversarial)입니다. Tester는 구현 전에 작성된 기준으로 검증합니다.

결과: 프로덕션 환경에서도 살아남는 기능.

## 트레이드오프

4x를 선택한다는 것은 구조와 정확성을 위해 속도와 비용을 교환한다는 의미입니다. 프로젝트에 그 교환이 필요한지 솔직하게 판단하세요.

### 강점

- **역할 분리로 셀프 리뷰 편향을 제거합니다.** Coder는 자신의 작업을 판단하지 않습니다. Reviewer는 설계부터 적대적입니다. 단일 에이전트 워크플로우는 같은 모델이 코드를 작성하고 승인하지만 — 4x는 그렇지 않습니다.
- **확정적 가드레일(guardrail)은 AI 판단에 의존하지 않습니다.** 범위 잠금, 상태 머신, 증거 요구사항 — 이들은 Go로 작성된 CLI에서 시행되며, LLM에게 "범위를 벗어나지 마세요"라고 프롬프트하는 것이 아닙니다.
- **파일 기반 프로토콜은 LLM에 구애받지 않습니다.** Claude, Gemini, Codex를 전환하거나 역할별로 혼합할 수 있습니다. 벤더 종속 없음, SDK 의존성 없음.
- **충돌 내성 상태.** 모든 것이 `.4x/` 파일에 저장됩니다. 세션이 종료되고 머신이 재부팅되어도 — `4x run`은 정확히 멈춘 곳에서 다시 시작합니다.
- **사람이 루프 안에 있습니다.** `pending-review` 게이트는 AI 작업이 완료로 표시되기 전에 항상 사람이 리뷰하도록 보장합니다. AI가 제안하고 당신이 결정합니다.
- **배치(batch) 모드로 확장 가능합니다.** 의존성 인식 스케줄링으로 수십 개의 기능을 밤새 대기열에 넣고 아침에 리뷰할 수 있습니다.

### 약점

- **토큰 비용이 상당히 높습니다.** 모든 기능은 최소 4회 이상의 별도 LLM 호출을 거칩니다. 리뷰 실패 시 두 배가 됩니다. 같은 작업에 대해 단일 에이전트 방식보다 3-10배의 토큰 비용을 예상하세요. [사용 팁](docs/guide/ko/usage-tips.md)에서 비용 추정치를 확인하세요.
- **간단한 작업에는 느립니다.** 한 줄짜리 버그 수정에 Designer, Reviewer, Tester가 필요하지 않습니다. 사소한 변경에는 전체 루프의 오버헤드가 낭비됩니다. 빠른 수정에는 단일 에이전트 도구를 사용하세요.
- **설정 비용.** `4x init`, 기능 YAML, 설정 구성 — 시작 전에 절차가 있습니다. 일회용 스크립트에는 가치가 없습니다.
- **고정된 루프 구조.** Design → Code → Review → Test 순서가 고정되어 있습니다. 워크플로우가 네 가지 역할에 맞지 않으면 프레임워크와 싸우게 됩니다.
- **품질은 프롬프트 품질에 달려 있습니다.** 모호한 기능 설명은 모호한 스펙을, 모호한 스펙은 잘못된 코드를 만듭니다. 4x는 구조를 추가하지만, 쓰레기를 넣으면 쓰레기가 나옵니다 — 단계가 더 많을 뿐입니다.

### 4x를 사용하기 좋은 경우

- 정확해야 하는 기능 (결제, 인증, 데이터 파이프라인)
- 적대적 리뷰가 필요한 작업 (보안 민감 코드)
- 기능 백로그의 배치 처리
- AI 생성 코드의 감사 추적이 필요한 팀

### 4x를 사용하지 않는 것이 좋은 경우

- 빠른 일회성 수정이나 탐색적 프로토타이핑
- 정확성보다 속도가 중요한 작업
- 토큰 예산이 빠듯한 프로젝트
- 어차피 직접 코드를 리뷰할 솔로 해킹 세션

## 아키텍처

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**레이어 1 — CLI**는 모든 확정적 작업을 처리합니다: 범위 검증, 상태 전환, 베이스라인 스냅샷, 증거 수집. LLM을 절대 호출하지 않습니다. 가드레일은 AI 판단에 의존하지 않습니다.

**레이어 2 — 러너(Runner)**는 CLI 프로토콜을 선택한 AI 도구에 연결합니다. Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor — 각각 같은 `.4x/` 파일 프로토콜을 사용하지만 네이티브 플랫폼 기능을 활용합니다.

**레이어 3 — Live**는 다중 프로젝트 대시보드입니다. AI 에이전트의 작업을 실시간으로 관찰하고, 단계 전환을 확인하며, 로그를 스트리밍합니다. REST + SSE API.

## 설치

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/4x
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 바이너리 다운로드

macOS, Linux, Windows (amd64 / arm64) 사전 빌드된 바이너리는 [Releases](https://github.com/ggwhite/4x/releases) 페이지에서 다운로드할 수 있습니다.

## 빠른 시작

```bash
# 프로젝트에서 초기화
cd my-project
4x init

# 기능 생성
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# 전체 루프 실행
4x run F001 --runner claude

# 상태 확인
4x status

# 리뷰 후 완료
4x done F001

# 또는 실시간으로 관찰
4x live -w
```

`4x run`은 Design-Code-Review-Test 루프를 자동으로 진행합니다. Review에서 문제가 발견되면 Code가 다시 실행됩니다. Test가 실패하면 루프가 반복됩니다. `--max-rounds`와 `--timeout` 플래그로 제어할 수 있습니다.

## 네 가지 역할

| 역할 | 작업 | 산출물 |
|---|---|---|
| **Designer** | 요구사항 분석, 스펙 + 인수 기준 작성 | `task-brief.md`, `acceptance-criteria.md` |
| **Coder** | 스펙에 따라 정확히 구현 | 소스 코드, `coder-report.md` |
| **Reviewer** | 버그 및 스펙 위반 포착 (체크리스트 + 적대적) | `review-report.md` (판정 포함) |
| **Tester** | 인수 기준에 대해 증거 기반 검증 | `test-report.md`, `verify.json` |

각 역할은 **격리**되어 있습니다. Coder는 Reviewer의 이전 피드백을 보지 못합니다. Tester는 Coder가 아닌 Designer가 작성한 기준으로 검증합니다. 이 분리는 단일 에이전트 워크플로우에서 발생하는 사각지대를 방지합니다.

## 루프 작동 방식

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **리뷰 실패** (판정 FAIL 또는 CRITICAL 발견)는 코드를 수정을 위해 다시 보냅니다
- **테스트 실패** (검증 미통과)는 코드를 수정을 위해 다시 보냅니다
- **에스컬레이션** (스펙 불일치, 기준 오류)은 Designer에게 다시 라우팅됩니다
- **Pending review** 게이트는 완료 표시 전에 항상 사람이 리뷰하도록 보장합니다
- **라운드 예산** (기본 5)으로 무한 루프를 방지합니다

## 확정적 가드레일

CLI에서 시행되며, AI 판단이 아닙니다:

| 가드레일 | 역할 |
|---|---|
| **범위 확인** | 변경된 파일이 선언된 리포지토리 내에 있어야 합니다 |
| **베이스라인 스냅샷** | 코딩 전 상태를 캡처하여 안전한 롤백 가능 |
| **상태 머신** | 단계가 합법적 순서로 진행되어야 합니다 |
| **증거 요구사항** | Tester가 커맨드 출력이 포함된 verify.json을 제공해야 합니다 |
| **테스트 게이트** | verify.json + test-report + final-report 필요 |
| **의존성 게이트** | 미충족 의존성이 있는 기능은 시작 불가 |

## 배치 모드

```bash
4x batch plan            # 의존성 인식 실행 계획 생성
4x batch run --runner claude  # 모든 적격 기능을 순서대로 실행
4x batch stop            # 현재 기능 완료 후 정상 종료
```

## 권한 모델

**4x는 AI 에이전트를 비대화형 모드로 실행합니다.** `4x init` 중에 러너는 권한 프롬프트를 건너뛰는 플래그(`--dangerously-skip-permissions`, `-y`, `approval: full-auto`)로 구성되어 루프가 자율적으로 실행됩니다.

CLI의 확정적 가드레일(범위 잠금, 베이스라인 스냅샷, 상태 머신)이 안전 경계를 제공합니다.

**4x는 자율 AI 에이전트 실행이 편안한 프로젝트에서만 사용하세요.**

## 문서

| 문서 | 설명 |
|---|---|
| **[사용자 가이드](docs/guide/ko/)** | 전체 사용 문서 |
| [시작하기](docs/guide/ko/getting-started.md) | 설치 및 첫 실행 |
| [CLI 레퍼런스](docs/guide/ko/cli.md) | 모든 명령어와 플래그 |
| [핵심 개념](docs/guide/ko/concepts.md) | 역할, 상태 머신, 프로토콜, 가드레일 |
| [설정](docs/guide/ko/configuration.md) | 설정, 모델, 로케일, 러너 |
| [러너 & 플러그인](docs/guide/ko/runners.md) | 지원되는 러너와 플러그인 계약 |
| [대시보드](docs/guide/ko/dashboard.md) | 4x Live 다중 프로젝트 대시보드 |
| [배치 모드](docs/guide/ko/batch.md) | 의존성 인식 배치 실행 |

## 프로젝트 구조

```
4x/
  cmd/4x/              CLI 진입점 (Cobra)
  internal/
    protocol/           .4x/ 파일 형식, 워크스페이스, 타입
    state/              상태 머신 (단계 전환)
    guard/              가드레일 검사 (범위, 베이스라인, 증거)
    batch/              의존성 DAG, 배치 스케줄러
    runner/             서브프로세스 러너 인터페이스
    server/             SSE + REST 서버 (Live 대시보드용)
  plugins/
    claude-code/        Claude Code 스킬 + 워크플로우
    codex/              Codex 러너 지침
    gemini/             Gemini 러너 지침
    agy/                Antigravity 러너 지침
    copilot/            Copilot 러너 지침 + 워크플로우
    cursor/             Cursor 규칙
    embed.go            go:embed로 플러그인 파일을 바이너리에 내장
  dashboard/
    macos/              Swift 네이티브 앱 (예정)
  docs/
    guide/              사용자 문서
    architecture/       시스템 수준 설계 문서
    design/             메커니즘 설계 문서
    reference/          플러그인 계약
```

## FAQ

**Q: 4x가 LLM API를 직접 호출하나요?**
아니요. CLI는 LLM 의존성이 없는 순수 Go입니다. 러너가 네이티브 플랫폼 기능을 사용하여 모든 AI 상호작용을 처리합니다.

**Q: 역할별로 다른 LLM을 사용할 수 있나요?**
네. `.4x/settings.json`에서 역할별 모델을 설정하세요. Design에는 Claude, Code에는 Gemini를 사용하는 식으로 — 각각 같은 `.4x/` 파일을 읽습니다.

**Q: Devin / SWE-agent / OpenHands와 어떻게 다른가요?**
그들은 모든 것을 한 번에 수행하는 자율 에이전트입니다. 4x는 확정적 가드레일을 갖춘 다중 역할 협업을 구조화하는 *프레임워크*입니다. 단일 자율 에이전트보다는 AI를 위한 CI 파이프라인에 가깝습니다.

## 탄생 배경

4x는 대규모 플랫폼 재작성을 위해 60개 이상의 기능을 출시한 DCT(Designer-Coder-Tester)라는 프로덕션 시스템에서 탄생했습니다. 살아남은 패턴들 — 역할 분리, 파일 기반 프로토콜, 확정적 범위 검사, 증거 기반 테스트 — 이 4x가 되었습니다. 살아남지 못한 부분들 — LLM 특화 핵, 공유 컨텍스트 가정, 신뢰 기반 가드레일 — 은 의도적으로 제외되었습니다.

## 기여하기

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## 라이선스

[MIT](LICENSE)

---

<p align="center">
  <strong>AI가 올바른 코드를 작성하기를 바라지 마세요. 검증을 시작하세요.</strong>
</p>
