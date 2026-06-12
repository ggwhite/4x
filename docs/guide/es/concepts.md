# Conceptos principales

## Los cuatro roles

| Rol | Responsabilidad | Entradas | Salidas | No puede |
|---|---|---|---|---|
| **Designer** | Analizar requisitos, producir spec, definir criterios de aceptación y estrategia de pruebas | Descripción del feature, codebase | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | Modificar código fuente |
| **Coder** | Implementar lo que dice la spec | `task-brief.md`, reportes previos de test/review | Código fuente, `coder-report.md` | Modificar criterios de aceptación o scripts de prueba |
| **Reviewer** | Detectar bugs, problemas de seguridad, violaciones de la spec | Diff, spec, reporte del coder, reglas del proyecto | `review-report.md` | Modificar código fuente |
| **Tester** | Validar contra criterios de aceptación con evidencia | Criterios de aceptación, reporte del coder, estrategia de pruebas | Scripts de prueba, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | Modificar código fuente |

Cada rol está **aislado** — el Coder nunca ve la retroalimentación previa de revisiones durante la implementación. El Tester valida contra criterios escritos por el Designer, no por el Coder.

### Review: dos fases

1. **Revisión con checklist** (modelo estándar) — verifica contra reglas estrictas del proyecto: seguridad, concurrencia, manejo de errores, estilo
2. **Revisión adversarial** (modelo profundo) — "¿Cuál es el peor bug escondido en este diff?" Los hallazgos se clasifican por severidad.

### Escalación

El Coder o Tester pueden escalar de vuelta al Designer cuando:

| Razón | Significado |
|---|---|
| `spec-mismatch` | La DB/API no coincide con la spec |
| `criteria-wrong` | Los criterios de aceptación son incorrectos |
| `blocker` | Falta una dependencia o problema de infraestructura |
| `scope-change` | Necesidad de modificar repos fuera del alcance |

La escalación se escribe en `escalation.json`. El ciclo redirige automáticamente al Designer.

---

## Máquina de estados

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     ├── amending ←──────────┘
                     ↑      ↓
                     └──────┘
```

### Todas las transiciones válidas

| Desde | Hacia |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`, `designing` |
| `reviewing` | `testing`, `amending` |
| `amending` | `reviewing`, `designing` |
| `testing` | `accepting`, `amending`, `designing` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding` |
| cualquiera | `blocked`, `needs-attention` |

### Contador de rondas

- Entrar a `coding` cuando la ronda es 0 establece la ronda en 1
- Entrar a `amending` incrementa la ronda
- `ShouldStop` se activa cuando la ronda >= maxRounds o 3+ rondas consecutivas sin progreso

### Decisiones de fase en el ciclo

| Fase | Condición | Acción |
|---|---|---|
| `designing` | `task-brief.md` faltante | → `needs-attention` |
| `coding` / `amending` | `escalation.json` con `spec-mismatch` o `criteria-wrong` | → `designing` |
| `reviewing` | Línea de veredicto comienza con FAIL o tiene `[CRITICAL]` | → `amending` |
| `testing` | `verify.json` no aprobado o artefactos faltantes | → `amending` |

---

## Protocolo de archivos

Los roles se comunican a través del directorio `.4x/`, no a través de ventanas de contexto compartidas.

```
.4x/
├── settings.json                    # Project config
├── plugins/                         # Runner instruction files
├── batch-plan.json                  # Batch execution plan
├── batch-stop                       # Graceful stop signal
├── features/
│   └── {id}.yaml                    # Feature definition (canonical source)
└── {feature-id}/
    ├── state.json                   # Phase, role, round, active, runner, stopReason
    ├── events.jsonl                 # Audit trail
    ├── baseline.json                # Pre-coding snapshot (HEAD, branch, dirty files)
    ├── task-brief.md                # Designer → Coder: spec + architecture
    ├── acceptance-criteria.md       # Designer → Tester: testable criteria
    ├── test-strategy.yaml           # Designer → Tester: test approach
    ├── final-report.md              # End-of-loop summary
    ├── commit-plan.md               # How to split changes into commits
    ├── logs/
    │   └── round-{N}-{role}.log     # Per-round per-role execution log
    └── rounds/round-{N}/
        ├── coder-report.md          # What the Coder did
        ├── review-report.md         # Reviewer findings + verdict
        ├── test-report.md           # Tester results
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status` refleja la fase de `state.json` para listado rápido. `depends` lista los IDs de features que deben estar terminados antes de que este feature pueda ejecutarse.

---

## Guardrails

Verificaciones determinísticas aplicadas por el CLI — no dependen del juicio de la IA.

| Guardrail | Qué hace |
|---|---|
| **Archivos requeridos** | Verifica que existan los artefactos apropiados para la fase (ej., `task-brief.md` después de designing) |
| **Baseline** | Captura el estado pre-codificación (HEAD, branch, archivos sucios); advierte si existen archivos sucios |
| **Alcance** | Compara los directorios de nivel superior de `git diff --name-only HEAD` contra los repos declarados del feature |
| **Dependencias** | Bloquea `4x run` si los features dependientes no están terminados |
| **Desvío del backlog** | Advierte cuando `.4x/features/*.yaml` y los espejos externos están desincronizados |
| **Compuerta testing → accepting** | Requiere `verify.json` (passed=true), `test-report.md`, `final-report.md`, `commit-plan.md` |

Ejecutar manualmente con `4x check <feature-id>`.

---

## Compuerta de pending review

El ciclo **no** va directamente a `done`. Después de accepting, el feature entra en `pending-review` — esperando que un humano revise el trabajo de la IA.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

Esto asegura que un humano siempre dé su aprobación antes de que un feature se considere completo.
