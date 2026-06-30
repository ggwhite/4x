# 배치 모드

여러 기능을 의존성 인식 순서로 실행합니다.

## 워크플로우

```bash
# 1. 실행 계획 생성
4x batch plan

# 2. 다음 작업 확인
4x batch next

# 3. 모든 적격 기능 실행
4x batch run --runner claude

# 4. 정상 종료 (현재 기능 완료 후)
4x batch stop
```

## 계획 수립

`4x batch plan`은 완료되지 않은 모든 기능을 분석하고 `.4x/batch-plan.json`을 생성합니다:

1. **의존성 DAG** — 기능의 `depends` 필드에서 방향 그래프 구축
2. **순환 감지** — 순환 의존성이 있으면 오류 발생
3. **Union-Find 클러스터링** — 리포지토리를 공유하는 기능을 그룹화
4. **위상 정렬** — 각 클러스터 내 기능을 순서대로 정렬
5. **체인 스케줄링** — 긴 의존성 체인을 분할 (최대 길이는 `--max-chain`으로 설정 가능)

```bash
# 계획 미리보기
4x batch plan --dry-run

# 체인 길이 제한
4x batch plan --max-chain 3
```

출력 예시:

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## 실행

`4x batch run`은 기능을 의존성 순서대로 순차적으로 실행합니다:

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- `--runner`는 선택 사항; 생략 시 워크스페이스 설정의 기본 러너로 폴백
- 커밋 전략 `"never"`(격리 없음) 또는 `"per-round"`(worktree 격리 — 각 라운드를 feature worktree 내에서 자동 커밋) 사용
- 기능 간에 `.4x/batch-stop` 파일 확인
- 의존성이 완료되지 않은 기능은 건너뜀(의존성이 `done`, `abandoned`, `ready-for-review` 상태여야 완료로 간주)
- 각 기능 실행 전 런타임에서 의존성을 재확인; 충족되지 않으면 기능을 `blocked`로 표시하고 건너뜀
- 두 번 실패한 기능(` needs-attention`, `blocked` 또는 `in-progress` 상태 유지)은 나머지 배치 실행에서 건너뜀
- 각 기능 완료 후 진행 상황 보고

기능이 `ready-for-review`에 도달하면 자동으로 main에 병합되고 `done`으로 표시됩니다. 다음 기능의 worktree는 업데이트된 main에서 시작합니다. 병합 충돌이 발생하면 배치가 일시 중지됩니다([병합 충돌](#병합-충돌) 참조). `--no-auto-merge`를 전달하면 기능이 `ready-for-review`에서 수동 리뷰를 위해 대기합니다.

```bash
# 자동 병합 없이 실행
4x batch run --runner claude --no-auto-merge
```

> **참고:** `batch run`은 내부적으로 항상 계획을 처음부터 새로 생성합니다(기존 `batch-plan.json` 무시). `batch plan`보다 더 엄격한 필터를 사용합니다 — `done`, `abandoned`, `ready-for-review` 상태의 기능은 실행에서 제외됩니다.

## 종료

```bash
4x batch stop
```

`.4x/batch-stop` 시그널 파일을 생성합니다. 배치는 현재 기능을 완료한 후 정상적으로 종료됩니다.

## 병합 충돌

자동 병합에서 충돌이 발생하면 배치가 일시 중지되고 `.4x/batch-conflict.json`에 기능, 충돌 리포(멀티 리포 모드), 영향받은 파일을 기록합니다. worktree가 보존되어 충돌을 해결할 수 있습니다. 신호 파일을 통해 [대시보드](dashboard.md)가 충돌을 표시하고 **배치 계속** 액션을 제공합니다 — 내부적으로 신호 파일을 지우고 `4x batch run`을 재시작합니다. CLI에서는 파일을 해결하고 `4x merge <id>`를 실행한 다음 `4x batch run`을 다시 실행하여 계속합니다. 충돌 파일은 모든 배치 실행 시작 시 자동으로 지워집니다.

## 실행 보고서

모든 배치 실행이 종료될 때 — 정상 완료, 중지, 인터럽트 또는 크래시 — `.4x/batch-report.json`이 기록됩니다. 보고서에는 전체 통계(total / completed / failed / remaining), 러너, 총 소요 시간, 각 기능의 이름/최종 상태/소요 시간/라운드 수/중지 사유가 포함됩니다.

`outcome` 필드는 실행이 어떻게 종료되었는지 캡처합니다:

- `completed` — 모든 기능 완료
- `stopped` — 정지 버튼(`.4x/batch-stop`)을 누르거나 자동 병합 충돌로 실행이 일시 중지됨
- `interrupted` — 배치 프로세스가 `SIGTERM`/`SIGINT`를 받음; 보고서에 실행 중이던 기능을 기록
- `crashed` — 배치 프로세스가 패닉; 보고서는 최선의 노력으로 기록되며 `panicMessage`를 포함

[대시보드](dashboard.md)는 배치가 실행 중이 아닐 때 이 파일을 읽어 기능별 상세 내용으로 확장 가능한 "마지막 배치 보고서" 요약 카드를 표시합니다.

## 진행 상황 확인

```bash
# 다음 기능 확인 (기능 ID 출력)
4x batch next

# 하위 작업 프런티어 정보 포함 JSON 출력
4x batch next --json

# 모든 기능 개요
4x status
```

`--json`과 함께 사용하면 하위 작업 의존성 프런티어 — 모든 의존성이 완료되어 작업 준비가 된 하위 작업 세트 — 를 포함하는 JSON 객체가 출력됩니다:

```json
{
  "featureId": "F044-subtask-frontier",
  "slot": 0,
  "subtaskFrontier": ["parse-depends", "build-dag"]
}
```

적격 기능이 없으면 `null`을 반환합니다.
