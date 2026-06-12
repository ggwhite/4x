# 4x Live 대시보드

AI 개발 루프의 실시간 모니터링.

## 대시보드 시작

```bash
# 최근 프로젝트로 시작
4x live

# 특정 프로젝트 열기
4x live /path/to/project1 /path/to/project2

# 커스텀 포트
4x live -p 8080

# 브라우저에서 자동 열기
4x live -w

# macOS 네이티브 앱 열기
4x live -a
```

## 다중 프로젝트 지원

대시보드는 여러 프로젝트를 동시에 지원합니다. 경로 인수 없이 실행하면 `~/.4x/recent-projects.json`에서 로드합니다(LRU, 최대 20개 항목).

## 서버 API

대시보드는 REST 및 SSE 엔드포인트를 노출합니다:

### REST

| 엔드포인트 | 메서드 | 설명 |
|---|---|---|
| `/api/tasks` | GET | 모든 기능 나열 |
| `/api/new` | POST | 새 기능 생성 |
| `/api/run` | POST | 기능 실행 시작 (`4x run` 서브프로세스 생성) |
| `/api/stop` | POST | 실행 중인 기능 중지 |
| `/api/done` | POST | 기능을 완료로 표시 |
| `/api/runs` | GET | 활성 실행 나열 |
| `/api/events/{id}` | GET | 기능의 이벤트 가져오기 |
| `/api/messages/{id}` | GET | 기능의 메시지 가져오기 |
| `/api/logs/{id}` | GET | 기능의 로그 파일 나열 |
| `/api/logs/{id}/{file}` | GET | 특정 로그 파일 가져오기 |
| `/api/projects` | GET | 등록된 프로젝트 나열 |
| `/api/projects` | POST | 프로젝트 추가 (즉석 초기화를 위한 `init: true` 지원) |
| `/api/projects` | DELETE | 프로젝트 제거 |
| `/api/browse` | GET | 폴더 선택기 |

### SSE (Server-Sent Events)

| 엔드포인트 | 설명 |
|---|---|
| `/sse/events/{id}` | 기능의 이벤트 스트리밍 (1초 폴링) |
| `/sse/logs/{id}` | 기능의 최신 로그 파일 스트리밍 |

### 다중 프로젝트 라우팅

여러 프로젝트가 있을 때 엔드포인트 앞에 `/api/project/{project-id}/...` 및 `/sse/project/{project-id}/...`가 붙습니다. 단일 프로젝트 모드는 하위 호환성을 위해 접두사 없는 경로를 사용합니다.

## 프로세스 관리

대시보드는 러너 서브프로세스를 관리합니다:

- 프로젝트 설정의 `max_concurrent_runs` 준수
- stdout/stderr를 run-output/run-error 이벤트로 캡처
- 정상 종료: SIGTERM → 5초 → SIGKILL

## 플랫폼

| 플랫폼 | 상태 |
|---|---|
| 웹 UI (내장) | 사용 가능 |
| macOS 네이티브 (Swift) | 예정 |
| Electron (Windows/Linux) | 예정 |
