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

- 커밋 전략 `"never"` 사용 (리뷰 후 수동으로 커밋)
- 기능 간에 `.4x/batch-stop` 파일 확인
- 의존성이 완료되지 않은 기능은 건너뜀
- 각 기능 완료 후 진행 상황 보고

## 종료

```bash
4x batch stop
```

`.4x/batch-stop` 시그널 파일을 생성합니다. 배치는 현재 기능을 완료한 후 정상적으로 종료됩니다.

## 진행 상황 확인

```bash
# 다음 기능 확인
4x batch next

# 모든 기능 개요
4x status
```
