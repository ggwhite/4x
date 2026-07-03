# Conceptos principales

## Los cuatro roles

| Rol | Responsabilidad | Entradas | Salidas | No puede |
|---|---|---|---|---|
| **Designer** | Analizar requisitos, producir spec, definir criterios de aceptación y estrategia de pruebas | Descripción del feature, codebase | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | Modificar código fuente |
| **Coder** | Implementar lo que dice la spec | `task-brief.md`, reportes previos de test/review | Código fuente, `coder-report.md` | Modificar criterios de aceptación o scripts de prueba |
| **Reviewer** | Detectar bugs, problemas de seguridad, violaciones de la spec | Diff, spec, reporte del coder, reglas del proyecto | `review-report.md` | Modificar código fuente |
| **Tester** | Validar contra criterios de aceptación con evidencia | Criterios de aceptación, reporte del coder, estrategia de pruebas | Scripts de prueba, `test-report.md`, `verify.json`, `final-report.md` | Modificar código fuente |

Cada rol está **aislado** — el Coder nunca ve la retroalimentación previa de revisiones durante la implementación. El Tester valida contra criterios escritos por el Designer, no por el Coder.

### Roles adicionales del ciclo

Dos roles adicionales operan más adelante en el ciclo:

| Rol | Fase | Responsabilidad |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | Revisión adversarial — busca los peores bugs en todo el diff |
| **Acceptor** | `accepting` | Agrega los problemas aún sin resolver en `final-report.md` para revisión humana |

El Acceptor usa su propia configuración de modelo dedicada (`roles.acceptor.model`) — distinta del Designer. En lugar de releer por completo los reportes de cada ronda, lee los reportes de review/test/deep-review de la ronda final más las escalaciones para detectar los problemas aún sin resolver.

### Perfiles de pipeline

Un **perfil de pipeline** selecciona qué roles se ejecutan para un feature dado, de modo que el trabajo simple omite roles en lugar de ejecutar siempre el pipeline completo de seis roles. Perfiles integrados:

| Perfil | Roles |
|---|---|
| `full` | designer, coder, reviewer, tester, deep-reviewer, acceptor |
| `normal` | coder, reviewer, tester, acceptor |
| `quick` | coder, reviewer |

`coder` siempre es obligatorio. Cuando se configuran `profiles`, el perfil se selecciona automáticamente según la prioridad del feature (mayor prioridad -> `full`, luego `normal`, luego `quick`); `--profile` sobrescribe la elección. Un rol que no está en el perfil activo se omite — el ciclo transiciona por las mismas aristas válidas sin invocar ese runner. Ver [Configuración](configuration.md) para las opciones `profiles`, `parallel_review_test` y `coder_model`.

### Review: dos fases

1. **Revisión con checklist** (modelo estándar) — verifica contra reglas estrictas del proyecto: seguridad, concurrencia, manejo de errores, estilo
2. **Revisión adversarial** (modelo profundo) — "¿Cuál es el peor bug escondido en este diff?" Los hallazgos se clasifican por severidad.

### Auto-reparación del deep review

Cuando el Deep Reviewer encuentra problemas bloqueantes, la fase `deep-reviewing` los repara **in situ** en lugar de enviar el trabajo de vuelta a través de `amending -> reviewing -> testing`. Dado que el Reviewer y el Tester ya aprobaron antes del deep review, repetir toda la cadena costosa (especialmente con el modelo profundo) es un desperdicio.

Dentro de la misma fase, el ciclo lanza dos sub-roles focalizados, repitiendo hasta que el reporte apruebe o se alcance un límite:

| Sub-rol | Modelo | Lee | Escribe | Alcance |
|---|---|---|---|---|
| **mini-coder** | modelo del coder | solo `## Issues` de `deep-review-report.md` (no `task-brief.md`) | código fuente, `coder-report.md` | solo los problemas que el deep reviewer identificó |
| **re-verifier** | modelo del reviewer | los problemas previos + el diff del mini-coder de esta iteración | `deep-reverify-{n}.md`, actualiza el `## Verdict` de `deep-review-report.md` | verifica que los problemas anteriores estén corregidos y que el nuevo diff no introduzca bugs |

La fase permanece en `deep-reviewing` durante todo el proceso — los sub-roles no son fases de la máquina de estados. Cuando el re-verifier confirma un PASS limpio, el ciclo avanza a `accepting`. El ciclo ejecuta como máximo `roles.deep-reviewer.max_fix_rounds` iteraciones (predeterminado 2); si el mini-coder edita archivos fuera del alcance del feature, o se alcanza el límite mientras sigue fallando, el feature escala a `needs-attention` con el reporte FAIL preservado.

### Deep review paralelo

El deep review cubre 11 ángulos distintos (corrección, calidad, convención, historial, retroalimentación, ...). Cuando `roles.deep-reviewer.parallel_reviewers` es mayor que 1, el ciclo distribuye los ángulos entre varios sub-revisores focalizados en lugar de pedir a un solo agente que cubra los 11. Esto refleja cómo `/code-review` divide una revisión por dimensión, reduciendo la presión de contexto y la deriva de atención de cada agente.

La distribución la maneja completamente el CLI de 4x — no depende de las capacidades de subagente o herramientas del LLM. La fase `deep-reviewing` sigue siendo una sola fase:

| Sub-rol | Modelo | Lee | Escribe |
|---|---|---|---|
| **sub-reviewer** (xN) | modelo profundo (`roles.reviewer.deep_model`) | el diff + su subconjunto de ángulos asignado | `deep-review-partial-{i}.md` |
| **synthesizer** | modelo synthesizer (`roles.synthesizer.model`, predeterminado tier `sonnet`) | el contenido completo de cada reporte parcial | `deep-review-report.md` |

Los ángulos se dividen uniformemente y sin solapamiento: con el valor predeterminado `parallel_reviewers: 3`, los grupos son `[1-4]`, `[5-8]`, `[9-11]` (corrección / calidad+convención / historial+retroalimentación). Configura `roles.deep-reviewer.angles_per_reviewer` para fijar el tamaño del grupo explícitamente; déjalo vacío para el balanceo automático `ceil(11/N)`. Los N sub-revisores se ejecutan en paralelo, luego un único synthesizer deduplica, arbitra conflictos y unifica la puntuación de confianza en el mismo formato de `deep-review-report.md` que el ciclo de auto-reparación y `parseReviewVerdict` ya consumen — así que todo lo que viene después queda sin cambios.

Cuando `parallel_reviewers` no está configurado o es `<= 1`, el ciclo vuelve al flujo original de agente único: un deep reviewer procesa los 11 ángulos y escribe `deep-review-report.md` directamente, sin reportes parciales ni synthesizer.

### Ángulos de deep review selectivos

Antes de despachar el deep review, 4x analiza los paths de archivos afectados por el diff y selecciona cuáles de los 11 ángulos ejecutar. El campo `angle_mapping` en `roles.deep-reviewer` mapea prefijos de path (por ej. `internal/state/`) y patrones de sufijo (por ej. `*_test.go`) a números de ángulo. Para cada archivo modificado gana el prefijo más largo que coincida (las reglas de prefijo tienen prioridad sobre las de sufijo); la unión de todos los ángulos que coincidan se convierte en el conjunto seleccionado. Cuando ningún archivo coincide con ninguna regla, se ejecutan los 11 ángulos como fallback de seguridad.

La selección se registra en `deep-review-angles.json` en el directorio de la ronda, incluyendo qué archivos coincidieron con qué reglas y qué ángulos aportó cada uno. Este artefacto también lo usa la recuperación de crashes para determinar el conteo parcial correcto.

Para forzar los 11 ángulos independientemente del mapeo:
- Pasar `--all-angles` a `4x run`
- Establecer `deep_review_all_angles: true` en el YAML del feature

El `angle_mapping` se puede personalizar en `settings.json` bajo `roles.deep-reviewer`; cuando no está configurado, un valor predeterminado integrado cubre el layout estándar del proyecto (`internal/state/`, `internal/protocol/`, `cmd/`, `docs/`, `templates/`, `dashboard/`, `*_test.go`).

### SubFase del deep review y recuperación de crashes

La fase `deep-reviewing` ejecuta varios pasos internos (sub-revisor → synthesizer → mini-coder → re-verificador), pero **no** son fases de la máquina de estados. Para que el progreso en vivo y la recuperación de crashes sean conscientes de *qué* paso está ejecutándose, `State` lleva un campo `subPhase` (`internal/protocol/state.go`) que solo es significativo mientras `phase == deep-reviewing`:

| `subPhase` | Paso | Se establece cuando |
|---|---|---|
| `reviewing` | los sub-revisores (o fallback de agente único) escanean el diff | al entrar al deep review |
| `synthesizing` | el synthesizer está fusionando los reportes parciales | synthesizer lanzado |
| `fixing` | el mini-coder está reparando problemas bloqueantes | mini-coder de auto-reparación lanzado |
| `reverifying` | el re-verificador está confirmando la corrección | re-verificador de auto-reparación lanzado |

`WriteState` impone un único invariante: cualquier escritura cuya `phase` no sea `deep-reviewing` limpia `subPhase` a cadena vacía (`omitempty` la mantiene fuera de `state.json` completamente). Así, salir del deep review — hacia `accepting`, `amending` o `needs-attention` — nunca deja una sub-fase obsoleta, independientemente de qué ruta de salida se tome.

En la recuperación de crash, `smartResumePhase` ya no reinicia el deep review desde cero cuando `deep-review-report.md` está incompleto. Inspecciona los artefactos en disco y reanuda desde el paso correcto:

- **Cualquier `deep-review-partial-{i}.md` faltante o incompleto** → reanudar en `reviewing`; el ciclo paralelo solo re-lanza los sub-revisores cuyos parciales faltan (`missingDeepPartials`), reutilizando el grupo de ángulos original de cada índice para que nada se reasigne.
- **Todos los parciales presentes pero el reporte incompleto** → reanudar en `synthesizing`; los sub-revisores se omiten y solo el synthesizer vuelve a ejecutarse.
- **Reporte completo pero con FAIL** → comportamiento sin cambios: enrutar a `amending` con `subPhase` limpio.

Un parcial se juzga completo por `deepPartialComplete` — el archivo existe, no está vacío, y contiene la sección centinela `## Statistics` que la plantilla del deep-reviewer siempre emite, para que un parcial escrito a medias nunca se confunda con uno terminado. Esta recuperación de mínima re-ejecución evita gastar de nuevo el (costoso) modelo profundo en pasos que ya completaron antes del crash.

### Features auto-descubiertos

Un deep reviewer a menudo detecta problemas que son reales pero **fuera del alcance del feature actual** — un bug latente, deuda técnica, una capacidad faltante. Sin un lugar donde registrarlos, esas observaciones quedan enterradas en el reporte. Cuando `auto_discover_features` está habilitado, el ciclo de ejecución los captura automáticamente.

El deep reviewer escribe cada candidato fuera de alcance como un bloque `[NEW-FEATURE] <título>` (seguido de una breve descripción) en la sección `## Discovered Issues` de `deep-review-report.md`. Después de un **PASS final del deep review** (los únicos dos caminos de retorno que llegan a `accepting` — PASS en primera pasada, y un re-verifier del ciclo de auto-reparación que cambia a PASS), el ciclo analiza esos bloques y, completamente en la capa del CLI (sin llamada a LLM):

- **Deduplica** cada candidato contra features existentes y contra candidatos ya conservados, usando una verificación de similitud por solapamiento de tokens (Jaccard).
- **Limita** la cantidad a `max_discovered_features` (predeterminado `3`); el resto se registra como limitado.
- **Crea** los candidatos conservados como nuevos YAMLs de feature (estado `not-started`, reutilizando la misma numeración que `4x new`), agregando un evento `feature-discovered` por cada creación.
- **Resume** el resultado (creados / omitidos-como-duplicado / limitados) en `.4x/run/{feature-id}/discovered-features.md`.

El paso es best-effort: cualquier error se registra y nunca bloquea la transición a `accepting`. Solo se ejecuta en el PASS final del deep review — las rondas intermedias y las rutas FAIL/`needs-attention` nunca lo alcanzan. Ver [Configuración -> Auto-descubrimiento de features](configuration.md#auto-discover-features) para la configuración.

### Minero de historial y pool de candidatos

Auto-Discovered Features solo se activa en un **PASS final del deep review**, y solo analiza los bloques `[NEW-FEATURE]` del `deep-review-report.md` de esa única ronda. La señal más rica — los *fallos* — nunca se cosecha: un `escalation.json`, un feature atascado en `needs-attention`/`abandoned`/`blocked`, o el mismo problema de revisor FAIL recurriendo en muchos features.

El comando `4x mine` cierra esa brecha. Escanea el **directorio completo** `.4x/` en busca de señales de fallo históricas y las agrega en un pool de candidatos en `.4x/candidates.json`. Es un comando puro de capa CLI/protocol — **sin llamada a LLM**, solo escaneo mecánico más la misma deduplicación Jaccard de solapamiento de tokens que usa Auto-Discovered Features. Tres escáneres alimentan el pool, etiquetando cada candidato con un `Source` y una cadena de trazabilidad `Origin`:

| Fuente | Señal | Formato de origen |
|---|---|---|
| `escalation` | el `escalation.json` de cada ronda con `needed: true`, clasificado por `reason` (spec-mismatch / criteria-wrong / blocker / scope-change) | `<featureID> round-<n> <reason>` |
| `stuck` | features cuya fase en `state.json` es `needs-attention`, `abandoned`, o `blocked`; el motivo de bloqueo se toma de `stopReason`/`stopMessage`, con fallback a la `detail` de escalación de la última ronda | `<featureID> <phase>` |
| `fail-pattern` | títulos de issues FAIL de reviewer/deep-reviewer que recurren en features **distintos** (múltiples rondas del mismo feature cuentan una vez), agrupados por similitud Jaccard y controlados por `--min-occurrences` (predeterminado `3`) | `N features: <ids>` |

Un fail-pattern recurrente también emite un `CandidateLearning` (categoría `review`) sugiriendo que el issue se promueva a una lista de verificación de revisión o plantilla.

El `CandidatePool` de salida (`candidates.json`) contiene `Version`, `GeneratedAt`, una lista de `Candidate`s y una lista de `CandidateLearning`s. Antes de escribir, los candidatos se deduplicarán de tres formas: contra los YAMLs de features existentes, contra el `candidates.json` anterior, y dentro del lote actual. Flags: `--min-occurrences` (umbral de fail-pattern), `--output` (predeterminado `.4x/candidates.json`), y `--dry-run` (imprimir el resumen sin escribir).

El comando completo es best-effort — un feature corrupto se registra y omite, nunca abortando el escaneo. Crucialmente, `4x mine` **solo produce el pool de candidatos; nunca crea features**. Si un candidato se promueve a un feature real lo deja a un gate separado (F097). Esto lo hace complementario a — no un reemplazo de — Auto-Discovered Features: uno cosecha notas en-alcance en el éxito, el otro cosecha señales de fallo a través de todo el historial.

### Evolve Driver

`4x evolve` conecta mine, el F097 value gate y enrichment en un bucle cerrado repetible: **mine → gate (pre → rol LLM gate → post) → enrich → enqueue → (opcional) auto-run → learnings alimentan la siguiente ronda**. La capa CLI se mantiene libre de LLM — tanto el rol gate como el enrichment se ejecutan como subprocesos `runner.Runner`, nunca como llamadas API en línea.

El orden del pipeline es **mine → gate → enrich → enqueue** (no mine → enrich → gate): el gate consume `Candidate`s sin procesar, por lo que el enrichment — que materializa un candidato en un `feature.Feature` completo — solo se ejecuta sobre los sobrevivientes del gate, sin desperdiciar costo de LLM en candidatos vetados. Los candidatos aceptados se encolan como feature YAMLs `not-started` (pasar el value gate **es** la aprobación; no hay un segundo paso draft→not-started). Si el enrichment falla o se descarta, el candidato aún se encola como un feature básico construido desde su texto descriptivo, marcado como `enriched=false` — el gate ya avaló su valor.

Cada llamada ejecuta exactamente **una ronda**; las rondas repetidas se manejan externamente (cron o invocación repetida). Cada ronda escribe `.4x/evolve-report.md` (Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted), que el dashboard muestra mediante `GET /api/evolve-report`.

**Detención anti-giro** previene que el bucle gire para siempre sin resultados. `.4x/evolve-state.json` persiste `consecutiveNoAccept` entre llamadas; una ronda que no acepta nada lo incrementa, una ronda que acepta algo lo reinicia a cero. Al alcanzar `evolution.max_idle_rounds`, la siguiente llamada se detiene antes del mining, marca el reporte como `Halted` y sale con código 0. Esta configuración distingue **no establecido** (`nil` → por defecto `3`) de un explícito `<= 0` (desactiva la detención — ejecutar siempre); `--force` anula una detención por una vez.

Con `--auto-run`, el meta-loop de cada feature encolado se ejecuta inmediatamente, siempre bajo el F098 self-mod scope guard: un feature que toca `self_mod_guard.protected_paths` sin aprobación no se auto-completa y se marca como `SelfModBlocked` en el reporte (resolver con `4x done --approve-self-mod`). `--dry-run` es solo lectura — imprime el resumen mine/dedupe y no escribe nada, no inicia runners ni crea features (e ignora `--auto-run` con una advertencia).

### Escalación

El Coder o Tester pueden escalar cuando:

| Razón | Significado | Redirige a |
|---|---|---|
| `spec-mismatch` | La DB/API no coincide con la spec | Designer |
| `criteria-wrong` | Los criterios de aceptación son incorrectos | Designer |
| `blocker` | Falta una dependencia o problema de infraestructura | `needs-attention` (intervención humana) |
| `scope-change` | Necesidad de modificar repos fuera del alcance | Designer |

La escalación se escribe en `escalation.json`. El ciclo redirige automáticamente `spec-mismatch`, `criteria-wrong` y `scope-change` de vuelta al Designer. Una escalación `blocker` va a `needs-attention` para intervención humana.

---

## Máquina de estados

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
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
| `testing` | `deep-reviewing`, `amending`, `designing` |
| `deep-reviewing` | `accepting`, `amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| cualquiera | `blocked`, `needs-attention`, `done`, `abandoned` |

### Contador de rondas

- Entrar a `coding` cuando la ronda es 0 establece la ronda en 1
- Entrar a `amending` incrementa la ronda
- `ShouldStop` se activa cuando la ronda >= maxRounds o 3+ rondas consecutivas sin progreso

### Decisiones de fase en el ciclo

| Fase | Condición | Acción |
|---|---|---|
| `designing` | `task-brief.md` o `acceptance-criteria.md` faltante | -> `needs-attention` |
| `coding` / `amending` | `escalation.json` con `spec-mismatch`, `criteria-wrong` o `scope-change` | -> `designing` |
| `reviewing` | Review no aprobado (requiere veredicto explícito `PASS` o `CONDITIONAL PASS` Y cero issues `[CRITICAL]`/`[WARNING]` en el reporte) | -> `amending` |
| `testing` | `verify.json` no aprobado o artefactos faltantes | -> `amending` |
| `deep-reviewing` | Deep review FAIL | auto-reparación in situ (mini-coder + re-verifier), hasta `max_fix_rounds`; PASS -> `accepting`, de lo contrario -> `needs-attention` |
| cualquiera (no-designer) | Verificación de guardrails encuentra violación de alcance, desvío de baseline o archivo requerido faltante | -> `needs-attention` |

---

## Protocolo de archivos

Los roles se comunican a través del directorio `.4x/`, no a través de ventanas de contexto compartidas.

```
.4x/
├── settings.json                    # Configuración del proyecto
├── plugins/                         # Archivos de instrucciones de runners
├── batch-plan.json                  # Plan de ejecución batch
├── batch-stop                       # Señal de detención controlada
├── batch-pid                        # PID del subproceso batch en ejecución (adopción de huérfanos del servidor)
├── batch-conflict.json              # Señal de conflicto de auto-merge del batch (pausado)
├── batch-report.json                # Último reporte de ejecución batch (estadísticas + resultado por feature)
├── features/
│   └── {id}.yaml                    # Definición del feature (fuente canónica)
└── run/                            # Artefactos de ejecución (directorios de trabajo por feature)
    └── {feature-id}/
        ├── state.json                   # Phase, role, round, active, runner, runners, stopReason, profile
        ├── events.jsonl                 # Registro de auditoría
        ├── baseline.json                # Instantánea pre-codificación (HEAD, branch, archivos sucios)
        ├── task-brief.md                # Designer → Coder: spec + arquitectura
        ├── acceptance-criteria.md       # Designer → Tester: criterios verificables
        ├── test-strategy.yaml           # Designer → Tester: enfoque de pruebas
        ├── final-report.md              # Resumen de fin de ciclo
        ├── logs/
        │   ├── round-{N}-{role}.log              # Log de ejecución por ronda por rol
        │   ├── round-{N}-deep-reviewer-{i}.log   # Por sub-revisor paralelo (cuando se distribuye)
        │   └── round-{N}-synthesizer.log         # Synthesizer fusionando los reportes parciales
        └── rounds/round-{N}/
            ├── coder-report.md            # Lo que hizo el Coder
            ├── review-report.md           # Hallazgos del Reviewer + veredicto
            ├── test-report.md             # Resultados del Tester
            ├── deep-review-partial-{i}.md # Hallazgos de un sub-revisor paralelo (cuando se distribuye)
            ├── deep-review-report.md      # Deep review fusionado (salida del synthesizer o agente único)
            ├── verify.json                # {passed, round, role, commands[]}
            └── escalation.json            # {needed, reason, detail}
```

### Archivos señal del batch

Dos archivos señal a nivel raíz coordinan un batch en ejecución con observadores externos (el CLI y el dashboard):

- **`batch-stop`** — un archivo marcador vacío. `4x batch run` lo verifica entre features y se detiene de forma controlada cuando existe (ver [Modo batch](batch.md)).
- **`batch-conflict.json`** — se escribe cuando el auto-merge del batch encuentra un conflicto y se pausa. Contiene suficiente detalle para que el dashboard muestre el conflicto sin re-ejecutar git:

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  `conflictRepo` está vacío en modo monorepo. El archivo se limpia al inicio de cada ejecución batch y cuando el usuario continúa un batch pausado.

- **`batch-report.json`** — se escribe cuando una ejecución batch termina (normalmente, por detención, interrupción o crash). A diferencia de los dos archivos señal anteriores, persiste entre ejecuciones como el "último reporte batch" que el dashboard muestra cuando no hay un batch activo. Registra el `outcome`, conteos generales (`total` / `completed` / `failed` / `remaining`), el runner, la duración total y un desglose por feature (estado final, rondas, razón de detención); un outcome `crashed` también lleva `panicMessage`. Se escribe de forma atómica (archivo temporal + renombrado) para que el dashboard nunca lea un reporte a medio escribir.

### Escrituras atómicas de estado

`state.json` es leído y escrito por múltiples actores concurrentemente — el ciclo de ejecución, el servidor del dashboard y los reconciliadores en segundo plano. Para evitar que un lector vea un archivo truncado o a medio escribir, `WriteState` nunca escribe in situ. Serializa el estado, lo escribe en un archivo temporal (`.state-*.json`) **en el mismo directorio** (garantizando el mismo sistema de archivos para que el renombrado sea atómico), luego ejecuta `os.Rename` sobre `state.json`. Un lector por lo tanto siempre ve el archivo antiguo completo o el nuevo completo — nunca uno parcial. Ante cualquier fallo, el archivo temporal se elimina para que no se acumulen residuos `.state-*.json`. No se usa bloqueo de archivo; la corrección proviene del renombrado atómico más la comparación de `UpdatedAt`.

### Recuperación del path del worktree

Cuando un feature se ejecuta en aislamiento de worktree, el ciclo imprime `worktree: <path>` al inicio, lo cual se registra en `events.jsonl` como un evento `run-output`. `Workspace.WorktreePath` recupera ese path más tarde (por ej. para el descubrimiento de capturas de pantalla) escaneando el rastro de auditoría en lugar de volver a ejecutar git.

El escaneo lee el **archivo completo** `events.jsonl` y retorna el path del **último** evento `run-output` que coincida. Esto importa para las re-ejecuciones: cada `4x run` agrega un nuevo evento `worktree: …`, por lo que el archivo acumula entradas durante la vida del feature. Leer solo las primeras líneas omitiría el path una vez que se acumulen suficientes eventos, o retornaría un worktree obsoleto que ya ha sido eliminado. Tomar el último match siempre produce el worktree de la ejecución más reciente.

### Resiliencia del descubrimiento de capturas de pantalla

`Workspace.DiscoverScreenshots` lee el `verify.json` de cada ronda para recopilar la evidencia de capturas de pantalla registrada por el Tester. El `verify.json` de una sola ronda puede terminar malformado — por ejemplo, códigos de escape ANSI sin escapar de la salida capturada de un subproceso que se filtran al archivo — lo cual haría fallar el parseo JSON. En lugar de propagar eso como un error fatal que tumba toda la llamada de descubrimiento (y con ella `4x status`/`4x check` para todo el feature), un fallo de parseo se trata como best-effort: esa ronda no aporta capturas de pantalla provenientes de verify.json, pero su número de ronda igual se rastrea, y la evidencia de todas las demás rondas — más el fallback de escaneo de directorio — se procesa con normalidad.

### Cache de lectura del workspace (servidor del dashboard)

El CLI es un proceso de corta duración: cada comando lee los archivos de `.4x/` que necesita una vez y termina, por lo que siempre usa un `*protocol.Workspace` simple. El servidor del dashboard (`4x live`) es lo opuesto — es de larga duración y cada solicitud API re-lee los mismos archivos. En un workspace multi-proyecto x multi-feature (ej. 5 proyectos x 50 features) una sola solicitud puede disparar cientos de parseos YAML/JSON.

Para evitar esto, el servidor envuelve cada workspace en un `*protocol.CachedWorkspace` (`internal/protocol/cached.go`), un cache en memoria basado en mtime sobre las operaciones de solo lectura declaradas por la interfaz `WorkspaceReader` (`internal/protocol/reader.go`):

- **`ReadConfig`** — cachea `settings.json`; `os.Stat` compara el mtime del archivo, re-parseando solo cuando cambia.
- **`ListFeatures`** — cachea la lista completa de features; `os.ReadDir` compara el conjunto de archivos `.yaml` y el mtime de cada uno, re-parseando solo cuando un archivo se agrega, elimina o modifica. Retorna una copia para que los llamadores puedan mutar libremente. Usa validación flexible: features con problemas de formato (ej. subtask status no reconocido) se incluyen con `Warnings` en lugar de omitirse silenciosamente.
- **`LoadFeature`** — cachea cada feature por id, indexado por el mtime del YAML. Usa validación estricta — cualquier problema de formato retorna un error.
- **`ReadState`** — intencionalmente **no** se cachea (cambia frecuentemente, archivo pequeño, parseo rápido); pasa directamente al `*Workspace` embebido.

La invalidación es implícita: los métodos de escritura (`SaveFeature`, `WriteState`, ...) no necesitan notificar al cache porque la próxima lectura detecta el nuevo mtime. El cache es opt-in — solo el servidor construye un `CachedWorkspace`; el CLI sigue usando `*Workspace` con comportamiento idéntico. Dado que el embedding de Go no tiene dispatch virtual, las llamadas internas de métodos de `*Workspace` (ej. `CompareBacklogMirror` llamando `w.ListFeatures()`) siguen ejecutando el original sin cache; esto es aceptable ya que esas rutas no son hot-paths del servidor.

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # numérico: 0-1 = perfil full, 2 = normal, 3+ = quick (omitir para nil/sin configurar)
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # ruta explícita opcional al design spec (sobrescribe la búsqueda en docs/design/)
plan: ""     # ruta explícita opcional al plan de implementación
hooks: {}    # hooks de fase opcionales (mismo formato que settings.json)
```

`status` refleja la fase de `state.json` para listado rápido. Valores válidos: `not-started`, `in-progress`, `ready-for-review`, `needs-attention`, `blocked`, `done`, `abandoned`. Los features `abandoned` se tratan como completados (no bloquean dependencias) pero se muestran con tachado en el dashboard. `depends` lista los IDs de features que deben estar terminados (o abandonados) antes de que este feature pueda ejecutarse. `repos` lista los nombres de repositorios (de `workspace.repos`) que este feature toca; vacío significa todos los repos en alcance.

#### Resolución de documentos de diseño

El panel de resumen del dashboard y la inyección de documentos de planificación de `4x prompt` localizan el spec/plan de un feature mediante un resolver compartido (`protocol.ResolveDesignDoc`), de modo que ambos siempre ven el mismo documento. Orden de resolución por tipo de documento (`spec`/`plan`):

1. El campo `spec`/`plan` del YAML del feature, leído como ruta (las rutas relativas se resuelven contra la raíz del workspace) cuando no está vacío.
2. `docs/design/{feature.ID}-{type}.md`.
3. `docs/design/{slug}-{type}.md`, donde `slug` elimina el prefijo `FNNN-` del ID (solo se intenta cuando difiere del ID).

El primer archivo existente gana; si ninguno coincide, el documento se trata como ausente.

### Creación de features

Los tipos `Feature`/`Subtask`/`Status` y la lógica de creación viven en el paquete independiente `internal/feature` (generación de ID, desvío de backlog, helpers de capturas de pantalla también se movieron allí). `protocol.Workspace` y `protocol.CachedWorkspace` satisfacen la interfaz `feature.Store`, y `feature` no importa `protocol` (dependencia unidireccional, desacoplada mediante `Store`). Tanto el CLI (`4x new`) como el dashboard (`POST /api/new`) crean features a través del único punto de entrada `feature.Create(store, opts)`, por lo que la numeración, truncamiento de ID y campos predeterminados se comportan de forma idéntica independientemente del punto de entrada.

### Configuración del workspace (multi-repo)

Por defecto, 4x opera en modo monorepo. Para trabajar con múltiples repositorios, declárelos en `.4x/settings.json`:

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

Cada entrada mapea un nombre de repo a su ruta (relativa a la raíz del workspace) y un flag `hub` opcional. Los repos hub son infraestructura compartida que múltiples features pueden tocar — se excluyen del agrupamiento por alcance en `4x batch plan`.

En modo monorepo (sin `workspace.repos`), todas las verificaciones de alcance y operaciones git usan la raíz del repo único.

### Guardia de auto-modificación

Cuando 4x se ejecuta sobre sí mismo (meta-loop), los cambios a su propia base central (máquina de estados / guardrails / protocol) son más riesgosos que el trabajo de feature ordinario — una regresión ahí rompe todo el ciclo multi-rol. La guardia de auto-modificación añade una capa extra sobre la guardia de Scope a nivel de repo, configurada bajo `self_mod_guard` en `settings.json`:

```json
"self_mod_guard": {
  "protected_paths": ["internal/state/", "internal/guard/", "internal/protocol/"],
  "max_diff_lines": 200,
  "require_tests": true
}
```

- `protected_paths` — lista de prefijos de path permitidos (relativa a la raíz del scope); los cambios bajo estos son marcados. Por defecto son las tres líneas rojas de arquitectura cuando no está configurado.
- `max_diff_lines` — presupuesto de diff protegido por ronda; excederlo falla la guardia y mueve el feature a `needs-attention`. Por defecto `200`.
- `require_tests` — cuando es `true` (predeterminado), los cambios `.go` protegidos deben incluir cambios `_test.go` protegidos antes de que el feature pueda salir de `testing`.

Un toque se detecta una vez durante la verificación de guardia post-coding y se persiste en `state.json` (`selfModTouched` / `selfModPaths`). Tocar paths protegidos nunca hace auto-merge: `4x done` / `4x merge` bloquean hasta que se vuelva a ejecutar con `--approve-self-mod`, lo cual registra `selfModApproved` en estado.

---

## Guardrails

Verificaciones determinísticas aplicadas por el CLI — no dependen del juicio de la IA.

| Guardrail | Qué hace |
|---|---|
| **Archivos requeridos** | Verifica que existan los artefactos apropiados para la fase (ej., `task-brief.md` después de designing) |
| **Baseline** | Captura el estado pre-codificación (HEAD, branch, archivos sucios); advierte si existen archivos sucios |
| **Alcance** | En modo monorepo: compara los directorios de nivel superior de `git diff --name-only HEAD` contra los repos declarados del feature. En modo multi-repo: usa `gitops.Ops.DetectChangedRepos()` a través de todos los repos del workspace |
| **Dependencias** | Bloquea `4x run` si los features dependientes no están terminados |
| **Desvío del backlog** | Advierte cuando `.4x/features/*.yaml` y los espejos externos están desincronizados |
| **Compuerta testing -> accepting** | Requiere `verify.json` (passed=true), `test-report.md`, `final-report.md` |

Ejecutar manualmente con `4x check <feature-id>`.

---

## Hooks de fase

Los hooks de fase permiten ejecutar comandos shell automáticamente antes o después de una transición de fase — útiles para levantar contenedores Docker, poblar bases de datos de prueba o limpiar después de las pruebas. Los hooks son ejecutados por el CLI, no por ningún rol de IA.

### Configuración

Los hooks se declaran en `settings.json` bajo la clave `hooks`. El formato de la clave es `pre_{phase}` o `post_{phase}`:

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

Cada entrada es un `HookEntry` con dos campos:

| Campo | Tipo | Descripción |
|---|---|---|
| `run` | string | Comando shell ejecutado vía `sh -c` |
| `on_fail` | string | `"block"` (predeterminado) o `"warn"` (insensible a mayúsculas) |

Los archivos YAML de features también pueden declarar un campo `hooks` con el mismo formato. Cuando un feature define hooks para la misma clave que la configuración global, la definición del feature **reemplaza** la global completamente (sin fusión dentro de una clave).

### Orden de ejecución

```
hooks pre_{target_phase} (en orden del array)
  ↓ si un hook on_fail=block falla → transición a needs-attention, abortar
state.Transition()
  ↓
registrar evento de transición
  ↓
hooks post_{target_phase} (en orden del array)
  ↓ si un hook on_fail=block falla → transición a needs-attention (sin rollback)
```

### Comportamiento ante fallos

| `on_fail` | Hook falla | Efecto |
|---|---|---|
| `block` (predeterminado) | hook pre | Feature movido a `needs-attention`; transición de fase abortada |
| `block` (predeterminado) | hook post | La fase ya cambió; feature movido a `needs-attention` |
| `warn` | cualquiera | Resultado registrado; la ejecución continúa |

### Registro

Cada ejecución de hook agrega un evento `type: "hook"` a `events.jsonl`:

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

La salida completa de stdout/stderr se escribe en `.4x/run/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`.

### Fusión de hooks (`MergeHooks`)

Los hooks globales y de feature se fusionan mediante `MergeHooks`: todas las claves globales se copian, luego las claves del feature sobrescriben las claves globales del mismo nombre completamente. Las claves que solo aparecen en la configuración global se preservan. Ambos nil retorna nil.

---

## Verificación de salud

Antes de que el rol Tester comience, el CLI puede verificar automáticamente que el entorno esté sano — que el build pase, los servicios estén activos, los endpoints respondan. Un entorno roto detectado aquí ahorra un ciclo completo de pruebas desperdiciado. Las verificaciones de salud las ejecuta el CLI, no ningún rol de IA, y se ejecutan solo al entrar en la fase `testing`, después de los hooks `pre_testing` y antes de iniciar el runner del Tester.

### Configuración

Una verificación de salud tiene tres campos (`HealthCheck` en `internal/protocol/verify.go`):

| Campo | Tipo | Descripción |
|---|---|---|
| `commands` | `[]string` | Comandos de verificación ejecutados en orden; cualquier fallo detiene la ejecución |
| `recovery` | `[]string` | Opcional. Se ejecutan en orden cuando un comando falla, para reparar el entorno |
| `timeout` | `int` | Timeout por comando en segundos; `<= 0` aplica el predeterminado `30` |

Se puede declarar globalmente en `settings.json` (JSON, sin yaml tag):

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

...o por feature en `test-strategy.yaml` (leído vía `Workspace.ReadTestStrategy`):

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**Fusión:** `ResolveHealthCheck` hace sobrescritura de grupo completo, no fusión a nivel de campo. Si `test-strategy.yaml` define `health_check`, reemplaza la global completamente; de lo contrario se usa la configuración global. Cuando ninguna está configurada, la verificación de salud se omite y el Tester comienza inmediatamente.

### Flujo de ejecución

```
se entra en la fase testing (hooks pre_testing ya ejecutados)
  ↓
ejecutar commands en orden (cada uno con su propio timeout)
  ├─ todos pasan → iniciar Tester
  └─ alguno falla →
      ├─ sin recovery → escalar a needs-attention
      └─ tiene recovery → ejecutar comandos de recovery en orden
          ├─ recovery falla → escalar a needs-attention
          └─ recovery pasa → re-ejecutar todos los commands una vez
              ├─ pasan → iniciar Tester
              └─ siguen fallando → escalar a needs-attention
```

La recuperación se ejecuta como máximo una vez — no hay reintentos múltiples ni backoff.

### Comportamiento ante fallos

Ante un fallo final, la ejecución registra un evento `type: "health-check-failed"` (rol `tester`, con el comando fallido y el error en `detail`), transiciona el feature a `needs-attention`, establece `StopReason` en `health-check-failed` y detiene el ciclo. Cada comando se ejecuta vía `sh -c` bajo un timeout por comando; un timeout cuenta como fallo y su salida se escribe a stderr para depuración.

---

## Perfiles de pruebas

Un **perfil de pruebas** es un bloque reutilizable de metodología de pruebas que el Designer asigna a un feature para que el prompt del Tester se inyecte automáticamente con la guía correspondiente — en lugar de mantener manualmente una lista gigante de `roles.tester.instructions` en `settings.json` que todos los features comparten independientemente de su tipo.

> No confundir con los **[perfiles de pipeline](#perfiles-de-pipeline)** (`Config.Profiles`), que seleccionan *qué roles se ejecutan*. Los perfiles de pruebas (`Config.TestProfiles`) inyectan *contenido de metodología de pruebas* solo en el prompt del Tester.

### Declaración de perfiles

El Designer lista los perfiles en `test-strategy.yaml` (`TestStrategy.Profiles` en `internal/protocol/verify.go`):

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` es `omitempty` — un `test-strategy.yaml` sin este campo se comporta exactamente como antes (sin inyección).

### Verificaciones manuales

Para los elementos de AC que necesitan verificación en tiempo de ejecución más allá de build/test/lint, el Designer puede añadir `manual_checks` a `test-strategy.yaml` (`TestStrategy.ManualChecks` en `internal/protocol/verify.go`):

```yaml
manual_checks:
  - id: mc-1
    ac_ref: AC-3
    description: "驗證 routing 正確分流"
    steps:
      - "啟動 server: go run ./cmd/gate --port 8080"
      - "curl http://localhost:8080/health → 確認 200"
  - id: mc-2
    ac_ref: AC-5
    description: "驗證 graceful shutdown"
    steps:
      - "啟動 server 並送 SIGTERM"
      - "確認 exit code 為 0"
```

El Tester debe ejecutar cada paso y registrar la salida real como evidencia en `verify.json` bajo `manual_check_results` (`VerifyEvidence.ManualCheckResults`). La guardia bloquea `testing → accepting` si alguna verificación manual no tiene resultado o tiene evidencia vacía. Si el fallo es reintentable, el tester obtiene un reintento automático con los errores de la guardia inyectados vía `guard-feedback.json`; un segundo fallo escala a `needs-attention`.

### Perfiles integrados

Cuatro perfiles vienen embebidos en el binario (`templates/profiles/*.md`, expuestos vía `templates.ProfilesFS`):

| Perfil | Metodología |
|---|---|
| `unit` | Go `go test`, aislamiento con `t.TempDir()`, table-driven, casos de error, verify.json por AC |
| `web` | Playwright contra el dashboard `4x live`; headless, workspace aislado + puerto aleatorio, capturas de pantalla como evidencia, sin interferencia con el servidor del usuario |
| `api` | Pruebas de endpoints HTTP — códigos de estado, cuerpo de respuesta, casos extremos, autenticación |
| `e2e` | Flujos end-to-end multi-servicio, estado de BD y consistencia entre servicios |

### Sobrescritura en settings.json

Un proyecto puede reemplazar o extender cualquier perfil vía `Config.TestProfiles` (`test_profiles`), indexado por nombre de perfil (`TestProfileOverride`):

```json
{
  "test_profiles": {
    "web": { "content": "Usar Cypress en lugar de Playwright para pruebas..." },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` — texto de reemplazo inline
- `include` — ruta (relativa a la raíz del workspace) a un archivo cuyo contenido se usa

**Orden de resolución** (por nombre de perfil): `test_profiles[name].content` -> `test_profiles[name].include` -> built-in `profiles/{name}.md`. La sobrescritura es un reemplazo completo, no fusión a nivel de campo. Un nombre desconocido (sin sobrescritura, sin built-in) imprime una advertencia a stderr y se omite.

El prompt del Tester renderiza cada perfil resuelto como un bloque `== Test Profile: {name} ==`. La carga se implementa en `loadProfiles` / `resolveProfileContent` (`cmd/4x/prompt.go`).

---

## Flujo de MR basado en issues

Configurar `"issue_tracker": {"enabled": true}` en `settings.json` (por defecto `false`, solo a nivel de proyecto) hace que `4x new`/`4x done` deleguen el code review a GitHub/GitLab en lugar de fusionar localmente. La plataforma (GitHub o GitLab, incluido self-hosted) se detecta automáticamente por repo según el hostname del remote `origin` — no hace falta configurar la plataforma por repo. Se requiere tener `gh`/`glab` instalado y autenticado.

- `4x new` ejecuta un preflight de `gh`/`glab` para cada repo declarado antes de crear la feature; cualquier fallo aborta la creación. Luego crea un issue nuevo por repo (o enlaza uno existente con `--issue "repo:id-or-url"`), registrando `{repo, id, url}` en el campo `issues` del YAML de la feature. Un fallo de creación/enlace en un repo se registra como advertencia (campo `warnings`, también impresa) y no bloquea la creación de la feature — el éxito parcial es aceptable.
- `4x done` hace push de la feature branch y abre un MR/PR (con `Closes #<issue-id>` en el cuerpo cuando existe un issue para ese repo) por cada repo con cambios confirmados, en lugar del squash-merge local. `done` pasa a significar "MR abierto, pendiente de revisión en la plataforma", no "fusionado" — nada hace polling ni espera la fusión real. Las URLs abiertas se imprimen como `MR opened[(repo)]: <url>` y aparecen como `mrUrls` en la salida `--json`. Si un repo falla al hacer push o al abrir su MR, la feature permanece en `pending-review` con el worktree preservado para reintentar (abrir un MR para una branch que ya tiene uno es idempotente — devuelve la URL del MR existente).
- Este flujo solo se activa desde un `4x new` explícito — el formulario "New Feature" del dashboard y las features auto-descubiertas/evolucionadas nunca crean issues.

Cuando `issue_tracker.enabled` es `false` (el valor por defecto), nada de lo anterior aplica: `4x new`/`4x done` se comportan exactamente como se documenta en el resto de esta página.

## Compuerta de pending review

El ciclo **no** va directamente a `done`. Después de accepting, el feature entra en `pending-review` — esperando que un humano revise el trabajo de la IA.

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

Esto asegura que un humano siempre dé su aprobación antes de que un feature se considere completo.
