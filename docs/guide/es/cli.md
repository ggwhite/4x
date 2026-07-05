# Referencia del CLI

Todos los argumentos de feature-id soportan coincidencia por prefijo insensible a mayúsculas. `4x run f001`, `4x run F001-user` y `4x run F001` resuelven a `F001-user-authentication-w`. Los prefijos ambiguos producen un error listando las coincidencias.

---

## `4x init`

Inicializar un workspace `.4x/` en el directorio actual.

```
4x init
```

- Detecta automáticamente el lenguaje del proyecto y los comandos de build/test/lint
- Crea `~/.4x/settings.json` con 6 runners predeterminados (claude, codex, gemini, agy, copilot, cursor)
- Despliega archivos de plugins embebidos en `.4x/plugins/`
- Agrega líneas `@import` a archivos del nivel raíz (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md, .cursorrules)
- Produce error si `.4x/` ya existe

### `4x init --dump-templates`

Vuelca las plantillas de prompts de rol integradas en `.4x/templates/` para que el proyecto pueda sobreescribirlas.

```
4x init --dump-templates          # escribir plantillas integradas en .4x/templates/
4x init --dump-templates --force  # sobreescribir archivos de plantilla existentes
```

- Requiere que `.4x/` ya exista (ejecutar `4x init` primero)
- Escribe cada `*.md.tmpl` embebido (incluyendo `locale.tmpl`) en `.4x/templates/`
- Los archivos existentes se omiten con una advertencia a menos que se use `--force`
- En tiempo de prompt, `.4x/templates/{file}` tiene prioridad sobre la plantilla embebida (sobreescritura completa); `locale.tmpl` y cada plantilla de rol regresan al valor por defecto de forma independiente

---

## `4x new <title>`

Crear un nuevo feature con metadatos opcionales.

```
4x new "Feature title" [flags]
```

| Bandera | Descripción |
|---|---|
| `--id` | Slug personalizado para el ID del feature (omite el truncamiento automático) |
| `--desc` | Descripción del feature (por defecto usa el título) |
| `--subtask` | Subtarea en formato `"id:name"` o `"id:name:description"` (repetible) |
| `--rule` | Referencia a regla (repetible) |
| `--depends` | ID de feature del que depende (repetible) |
| `--priority` | Nivel de prioridad (0=crítica, 1=alta, 2=media, 3=baja) |
| `--repo` | Repositorio dentro del alcance (repetible) |
| `--json` | Salida en formato JSON |

Crea `.4x/features/F{NNN}-{slug}.yaml` con estado `not-started`.
El slug generado automáticamente se trunca en el límite de palabra; usa `--id` para sobrescribirlo.
La creación pasa por la ruta compartida `feature.Create` (ver [Conceptos](concepts.md#creacion-de-features)) — el endpoint `POST /api/new` del dashboard usa la misma lógica, por lo que las banderas aquí se corresponden uno a uno con el formulario de nuevo feature del dashboard.

Ejemplos:
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

---

## `4x run <feature-id>`

Ejecutar el ciclo Design-Code-Review-Test para un feature.

```
4x run <feature-id> [flags]
```

| Bandera | Predeterminado | Descripción |
|---|---|---|
| `--runner` | predeterminado en config | Nombre del plugin runner |
| `--max-rounds` | `5` | Iteraciones máximas del ciclo |
| `--timeout` | `3600` | Timeout por fase en segundos |
| `--dry-run` | `false` | Imprimir prompts de roles sin llamar al LLM |
| `--json` | `false` | Iniciar ejecución y devolver JSON inmediatamente |
| `--profile` | auto | Perfil de pipeline (`full`/`normal`/`quick` o personalizado); sobrescribe la selección automática por prioridad |

`--profile` selecciona qué roles se ejecutan. Perfiles integrados: `full` (los 6 roles), `normal` (coder/reviewer/tester/acceptor), `quick` (coder/reviewer). Los roles que no están en el perfil se omiten (el estado avanza por la arista legal sin invocar el runner). Cuando se omite, el perfil se selecciona automáticamente según la prioridad del feature si existe una sección `profiles` en `settings.json` (de lo contrario, `full`). Ver [Configuración -> Perfiles](configuration.md#profiles) para más detalles.

El ciclo ejecuta: init -> designing -> design-reviewing -> coding -> reviewing -> testing -> deep-reviewing -> accepting -> pending-review. En fallo de review, code recibe otra pasada. En fallo de test, el ciclo re-entra en coding.

Después de que cada runner (excepto el designer) finaliza, se aplican automáticamente las verificaciones de guardrails (alcance, baseline, archivos requeridos). Una violación transiciona el feature a `needs-attention` y detiene el ciclo. El designer está exento ya que no modifica código fuente.

Los veredictos de review deben comenzar con `PASS` para aprobar. Las líneas en blanco entre el encabezado `## Verdict` y el texto del veredicto se ignoran. Una salida ambigua (`TODO`, `ERROR`, texto ilegible, bloque `## Verdict` faltante) se trata como fallo.

Los hooks de fase declarados en `settings.json` o en el YAML del feature se ejecutan automáticamente antes y después de cada transición de fase dentro del ciclo. Ver [Hooks de fase](concepts.md#hooks-de-fase) para detalles de configuración.

Al entrar en la fase `testing` (después de los hooks `pre_testing`, antes de iniciar el Tester), se ejecuta una verificación de salud del entorno si `health_check` está configurado. Los comandos de verificación se ejecutan en orden; en caso de fallo se ejecutan los comandos de recuperación una vez y se reintentan las verificaciones. Si el entorno sigue fallando, el feature transiciona a `needs-attention` y el ciclo se detiene. Ver [Verificación de salud](concepts.md#verificacion-de-salud) para detalles de configuración.

Cuando `auto_discover_features` está habilitado en `settings.json`, un deep review final con **PASS** analiza los marcadores `[NEW-FEATURE]` en `deep-review-report.md` y crea automáticamente YAMLs de features para los problemas fuera de alcance que el deep reviewer identificó (deduplicados y con límite). Ver [Configuración -> Auto-descubrimiento de features](configuration.md#auto-discover-features) y [Conceptos -> Features auto-descubiertos](concepts.md#features-auto-descubiertos) para más detalles.

Si el feature está en fase `blocked` o `needs-attention`, se recupera automáticamente a la fase de reanudación apropiada según el rol actual.

Verifica automáticamente la compuerta de dependencias — bloquea si los features dependientes no están completados.

Si `isolation: "worktree"` está configurado, ejecuta en un git worktree bajo `.worktrees/4x/<feature-id>/`. En modo multi-repo (workspace.repos configurado), cada repo obtiene su propio worktree bajo `.worktrees/4x/<feature-id>/<repo-name>/`, y los archivos a nivel de workspace (go.work, Makefile, etc.) se copian junto a ellos. Los prompts del coder incluyen una sección `== Workspace Repos ==`; en modo worktree, cada entrada muestra el nombre del repo como ruta relativa (ej. `core -> core/`) para que el coder opere dentro de los límites correctos del directorio.

---

## `4x status [feature-id]`

Mostrar estado de features.

```
4x status              # todos los features, agrupados por estado
4x status <feature-id> # detalles de un feature con subtareas
4x status --pending    # ocultar features done/abandoned
4x status --json       # salida en formato JSON
```

| Bandera | Descripción |
|---|---|
| `--pending` | Ocultar features done/abandoned |
| `--json` | Salida en formato JSON |

Grupos: Running, Review, Pending, Todo, Done (done muestra máximo 5). Incluye advertencias de desvío del backlog.

Para detalle de un feature individual (`4x status <feature-id>`), si existen capturas de pantalla, también muestra:

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x subtask <feature-id> <subtask-id>`

Actualizar el estado de una subtarea dentro de un feature.

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| Bandera | Descripción |
|---|---|
| `--status` | Nuevo estado: `done`, `in-progress`, `blocked`, `not-started`, `ready-for-review` (requerido) |

Ejemplo:
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x approve <feature-id>`

Aprobar un feature en estado `draft` producido por el auto-descubrimiento enriquecido, transicionándolo `draft → not-started` para que el meta-loop lo recoja. Los borradores solo se crean cuando `enrich_discovered_features` está habilitado y `enrich_auto_approve` es `false`. Produce error si el feature no está en estado `draft`.

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

Rechazar un feature en estado `draft` producido por el auto-descubrimiento enriquecido, transicionándolo `draft → abandoned` para que quede fuera del meta-loop. Produce error si el feature no está en estado `draft`.

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

Recuperar un feature atascado en `needs-attention` o `blocked` transicionándolo de vuelta a una fase activa, e inmediatamente lanzando `4x run`. Equivalente a `4x transition --to <phase> <id> && 4x run <id>`.

La fase destino por defecto es `accepting` (volver a ejecutar el Acceptor después de que el humano resuelva los problemas). Usa `--to` para apuntar a una fase diferente.

```
4x retry F042-some-feature
4x retry F042-some-feature --to amending
```

| Bandera | Descripción |
|------|-------------|
| `--to <phase>` | Fase destino para la recuperación (por defecto: `accepting`) |

Produce error si el feature no está actualmente en `needs-attention` o `blocked`.

---

## `4x gate`

Aplica las capas de veto del **value gate** F097 evolve a las features candidatas minadas. Veto CLI puro determinístico — no llama a un LLM. El rol LLM `gate` se ejecuta entre las dos fases (orquestado por el evolve driver) y produce `gate-verdicts.json`.

Se debe especificar exactamente uno de `--pre` / `--post`:

- `--pre` — PRE-veto: lee `.4x/candidates.json`, descarta candidatos con similitud Jaccard a features existentes (y duplicados intra-lote), escribe los sobrevivientes en `.4x/gate-input.json`.
- `--post` — POST-veto: lee `.4x/gate-input.json` + `.4x/gate-verdicts.json`, aplica el veto duro no anulable (non-accept / falta `why_not_hack` / por debajo de `value_floor` / duplica existente / excede `max_accept_per_run` / excede `max_backlog_undone`), escribe los candidatos aceptados (con `value_score`/`why_not_hack`) en `.4x/accepted-candidates.json`.

Los umbrales provienen de la sección `evolution` de `settings.json` (`value_floor`, `max_accept_per_run`, `max_backlog_undone`, `dedup_threshold`).

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

Ejecuta una ronda del pipeline de auto-mejora continua, conectando las piezas de evolución existentes en un bucle cerrado repetible:

**mine → gate (pre → rol LLM gate → post) → enrich → enqueue → (opcional) meta-loop auto-run → learnings alimentan la siguiente ronda.**

La capa CLI nunca llama directamente a un LLM — tanto el rol gate como el enrichment se ejecutan como subprocesos `runner`. Cada llamada ejecuta exactamente **una ronda**; las rondas repetidas se manejan externamente (cron o llamadas repetidas a `4x evolve`). Cada ronda escribe un resumen en `.4x/evolve-report.md`.

Pasos del pipeline:

1. **mine** — escanea `.4x/` buscando señales de fallo (escalaciones / features atascados / patrones FAIL recurrentes), deduplica y fusiona en `.4x/candidates.json`.
2. **gate pre** — deduplicación Jaccard de sobrevivientes en `.4x/gate-input.json`.
3. **gate role** — inicia el rol LLM `gate` para escribir `.4x/gate-verdicts.json`.
4. **gate post** — aplica el veto no anulable + topes de convergencia, escribe `.4x/accepted-candidates.json`.
5. **enrich + enqueue** — materializa cada candidato aceptado en un feature YAML `not-started` (en caso de fallo del enrichment, recurre a un feature básico creado desde el texto del candidato, marcado como `enriched=false`).
6. **auto-run** (opcional) — ejecuta el meta-loop para cada feature encolado, protegido por el F098 self-mod scope guard.

Anti-giro: cuando una ronda no acepta nada, `.4x/evolve-state.json` incrementa `consecutiveNoAccept`; al alcanzar `evolution.max_idle_rounds` (por defecto 3; `<= 0` para desactivar), la siguiente llamada se detiene anticipadamente, marca el reporte como `Halted` y sale con código 0. Use `--force` para anular.

```
4x evolve                        # ejecutar una ronda, features quedan en not-started
4x evolve --dry-run              # solo lectura: imprime resumen mine/dedupe, no escribe archivos
4x evolve --auto-run             # también ejecuta el meta-loop para features encolados
4x evolve --force                # anular la detención anti-giro
```

| Flag | Descripción |
|---|---|
| `--auto-run` | Ejecutar el meta-loop para cada feature encolado (F098 self-mod guard siempre forzado) |
| `--dry-run` | Análisis solo lectura: imprime cantidades mined/deduped, sin escritura de archivos, sin iniciar runner, sin crear features |
| `--min-occurrences` | Umbral de distinct-feature para que un patrón de fallo se convierta en candidato (por defecto 3) |
| `--force` | Anular la detención anti-giro y ejecutar incluso después de rondas inactivas consecutivas |
| `--runner` | Plugin runner para gate / enrich / auto-run (por defecto `evolution.gate_runner` o el predeterminado del proyecto) |
| `--timeout` | Timeout del subproceso LLM en segundos (por defecto 3600) |
| `--max-rounds` | Máximo de rondas por feature cuando se usa `--auto-run` (por defecto 5) |

El dashboard muestra el último reporte mediante `GET /api/evolve-report`.

---

## `4x check <feature-id>`

Ejecutar verificaciones de guardrails sin transicionar estado.

```
4x check <feature-id> [--json]
```

| Bandera | Descripción |
|---|---|
| `--json` | Salida de resultados en JSON |

Verifica: archivos requeridos, baseline, alcance, dependencias, desvío del backlog. Código de salida 0 si pasa, 1 si falla.

---

## `4x doctor`

Ejecuta una verificación de salud de solo lectura sobre la configuración fusionada (`.4x/settings.json` + `~/.4x/settings.json`) y la integridad del workspace, antes de iniciar una ejecución. Nunca llama a un LLM y no requiere que ningún runner esté instalado.

```
4x doctor [--json]
```

| Bandera | Descripción |
|---|---|
| `--json` | Salida del reporte completo en JSON (para CI) |

Las verificaciones se agrupan en secciones:

- **settings** — `settings.json` cargable, `project.name` no vacío, al menos un runner definido, `default_runner` existe en el mapa de runners.
- **runners** — el `command` de cada runner es resoluble en `PATH` (faltante -> WARN, no FAIL, ya que un runner puede residir en una máquina remota).
- **roles** — resuelve el modelo real que usará cada rol (designer/coder/reviewer/tester/acceptor) mediante el runner predeterminado, además del `deep_model` del reviewer.
- **workspace** — worktrees huérfanos (feature done/abandoned pero `.worktrees/4x/<id>` persiste), worktrees colgantes (directorio sin feature correspondiente), estado obsoleto (`active=true` pero el proceso ya no existe), y YAML de features malformados.

Cada línea tiene como prefijo `✅` (PASS), `⚠️` (WARN) o `❌` (FAIL), seguido de un conteo resumen.

Código de salida: `0` cuando no hay FAIL (WARN no afecta el código de salida), `1` cuando alguna verificación falla. `doctor` es estrictamente de solo lectura — nunca reescribe `state.json`, limpia worktrees ni modifica la configuración.

```bash
# Compuerta CI: fallar el build si hay algún FAIL
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

Ejecutar los comandos de verificación del `test-strategy.yaml` del feature y escribir los resultados en `rounds/round-{N}/verify.json`.

Los comandos pueden organizarse en grupos mediante `verify_groups`: los grupos se ejecutan en paralelo, mientras que los comandos dentro de un grupo se ejecutan secuencialmente. Si un comando en un grupo falla, los comandos restantes de ese grupo se omiten, pero los demás grupos continúan ejecutándose. Cuando solo se define `verify_commands`, se usa un grupo secuencial `default`. Declarar ambos es un error.

La ejecución paralela la maneja completamente el CLI — sin intervención de LLM. El rol Tester llama a este comando en lugar de ejecutar los comandos de verificación por sí mismo; los humanos también pueden ejecutarlo de forma independiente para depuración.

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| Bandera | Descripción |
|---|---|
| `--round` | Número de ronda (predeterminado: ronda actual de state.json) |
| `--timeout` | Timeout general para todos los grupos (predeterminado: 5m) |
| `--json` | Salida del verify.json completo en JSON |

Código de salida 0 cuando todos los comandos no omitidos pasan, 1 cuando alguno falla.

---

## `4x transition <feature-id>`

Forzar una transición de estado.

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| Bandera | Descripción |
|---|---|
| `--to` | Fase objetivo (requerido) |
| `--role` | Rol que realiza la transición |
| `--json` | Salida en formato JSON |

Valida que la transición sea legal según la máquina de estados. Auto-inicializa el estado si no existe. La transición `testing -> accepting` ejecuta compuertas adicionales (verify.json, test-report.md, final-report.md deben existir y la verificación debe aprobar).

Si `settings.json` o el YAML del feature declara `hooks`, los hooks `pre_{phase}` se ejecutan antes de la transición y los hooks `post_{phase}` se ejecutan después. Un fallo en un hook `block` de tipo pre aborta la transición; un fallo en un hook `block` de tipo post mueve el feature a `needs-attention`. Ver [Hooks de fase](concepts.md#hooks-de-fase) para el formato de configuración completo.

---

## `4x event <feature-id>`

Agregar un evento a `events.jsonl`.

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| Bandera | Descripción |
|---|---|
| `--type` | Tipo de evento (requerido) |
| `--role` | Rol que disparó el evento |
| `--round` | Número de ronda |
| `--action` | Nombre de la acción |
| `--detail` | Texto de detalle adicional |

---

## `4x prompt <feature-id>`

Imprimir el prompt del rol para un feature.

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| Bandera | Descripción |
|---|---|
| `--role` | Rol objetivo (inferido del estado actual si se omite) |
| `--round` | Número de ronda |

Soporta inyección de locale (desde la configuración del usuario o la variable de entorno `LANG`), inclusión automática de documentos de planificación e includes de proyecto/rol. Los documentos spec/plan se localizan mediante el resolver compartido (`protocol.ResolveDesignDoc`) — primero el campo `spec`/`plan` del YAML del feature, luego `docs/design/{id}-{type}.md`, luego el fallback `docs/design/{slug}-{type}.md` sin el prefijo `FNNN-` — por lo que el prompt ve los mismos documentos que el panel de resumen del dashboard. Ver [Resolución de documentos de diseño](concepts.md#resolucion-de-documentos-de-diseno).

Para el rol `tester`, cualquier `profiles` listado en el `test-strategy.yaml` del feature se resuelve (mediante `loadProfiles`) y se inyecta en el prompt como bloques `== Test Profile: {name} ==`. El contenido de cada perfil proviene de `settings.json` `test_profiles[name]` (`content` o `include`) cuando está presente, de lo contrario del built-in `templates/profiles/{name}.md`. Ver [Perfiles de pruebas](concepts.md#perfiles-de-pruebas).

---

## `4x done <feature-id>`

Marcar un feature en pending-review como terminado. Si el feature tiene un worktree (`.worktrees/4x/<id>`), automáticamente fusiona el branch de vuelta a main y elimina el worktree y el branch.

```
4x done <feature-id>
```

Solo funciona cuando el feature está en fase `pending-review`. Produce error en cualquier otra fase.

Si ocurre un conflicto de merge o error de merge, el feature permanece en `pending-review`, el worktree se preserva y se muestra orientación. En modo multi-repo, el nombre del repo en conflicto se muestra como `repo: <name>`. Usa `4x merge <id>` para completar después de resolver los conflictos.

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->

Forzar un feature a done desde cualquier fase no terminal. Requiere `--reason` para documentar por qué se está saltando el pipeline normal.

```
4x force-done <feature-id> --reason "código revisado y tests pasan, test e2e diferido a post-merge"
```

Transiciona el feature a `pending-review`, registra un evento `force-done` con el motivo, luego activa el mismo flujo de merge que `4x done`. Funciona desde `needs-attention`, `blocked`, o cualquier fase activa.

El dashboard expone esto como `POST /api/force-done` con `{id, reason}`.

| Bandera | Descripción |
|---|---|
| `--reason` | Por qué el feature está siendo forzado a completado (requerido) |
| `--json` | Mostrar resultado como JSON |

---

## `4x merge <feature-id>`

Completar un merge después de resolver conflictos de `4x done`.

```
4x merge <feature-id>
```

Solo funciona cuando el feature está en fase `pending-review` o `done` y existe un worktree en `.worktrees/4x/<id>`. Hace commit de los conflictos resueltos en el worktree, fusiona a main, luego elimina el worktree y el branch. Si el feature aún está en `pending-review`, se marca como `done` después de que el merge sea exitoso.

En modo multi-repo, los conflictos resueltos se comitean por repo (cada repo bajo `.worktrees/4x/<id>/<repo-name>/` se agrega al stage y se comitea independientemente), luego todos los repos se fusionan en modo todo-o-nada. El nombre del repo en conflicto se muestra como `repo: <name>` si un conflicto reaparece.

---

## `4x clean [feature-id]`

Eliminar artefactos del workspace (`logs/`, `rounds/`, reportes, `state.json`, `events.jsonl`) de features completados, liberando espacio en disco. Las definiciones de features (`.4x/features/*.yaml`) y el estado del feature siempre se preservan.

```
4x clean              # listar features limpiables + tamaños, confirmar, luego limpiar
4x clean --dry-run    # solo listar, no eliminar nada
4x clean --force      # omitir prompt de confirmación
4x clean <feature-id> # limpiar un solo feature (debe estar en done/abandoned)
```

Solo los features en estado `done` o `abandoned` con un directorio de workspace existente son elegibles. Los features activos (en ejecución) nunca se limpian, y los features en `blocked` / `needs-attention` se mantienen para que sus artefactos de depuración permanezcan disponibles. La limpieza no es una transición de la máquina de estados — no cambia el ciclo de vida del feature.

---

## `4x learn`

Gestionar los aprendizajes retro — lecciones de desarrollo acumuladas a través de features en `.4x/learnings.json`.

El Acceptor de cada feature escribe un `retro-learnings.json`; el CLI lo cosecha en `.4x/learnings.json`. Al generar el prompt de cada rol, el CLI filtra directamente `.4x/learnings.json` por la categoría de ese rol (con cupos por bucket active/candidate) e inyecta el resultado — no hay un paso intermedio donde un Designer seleccione primero. Los learnings son gestionados enteramente por el CLI — los runners nunca escriben `learnings.json` directamente, y cualquier fallo de learnings solo advierte sin bloquear las transiciones de estado.

```
4x learn add --category <cat> --content <text>  # agregar learning manualmente (sesiones standalone)
4x learn add --category ops --content "..." --json  # salida JSON: {"id":"L0xx","added":true}
4x learn list                     # listar learnings activos + candidatos (por defecto)
4x learn list --category=testing  # filtrar por categoría
4x learn list --status=active     # filtrar por estado (active, candidate, stale, promoted)
4x learn list --ineffective       # solo mostrar entradas ineficaces (used≥3 + 30d + misma categoría)
4x learn prune                    # marcar entradas obsoletas (>90 días sin uso) y eliminarlas
4x learn prune --dry-run          # previsualizar entradas obsoletas sin eliminar
4x learn promote <id>             # marcar un learning como promovido (se mantiene pero no se inyecta)
4x learn remove <id>              # eliminar una entrada de learning
```

`learn add` verifica si hay entradas similares existentes (coincidencia exacta, normalizada y similitud Jaccard). Si se encuentra un duplicado aproximado, reporta el ID existente y no escribe.

- Categorías: `design`, `code-quality`, `testing`, `review`, `tooling`, `process`, `ops`
- Estado: `active` (inyectable), `candidate` (nuevo harvest, pendiente de validación cross-feature), `stale` (>90 días sin uso, marcado automáticamente al leer), `promoted` (actualizado a plantilla/instrucciones)
- Las entradas candidatas se muestran con sufijo `*` en el ID; se promueven automáticamente a active cuando son producidas independientemente por otra feature o seleccionadas por un Designer
- Las entradas ineficaces son learnings activos marcados con estado `active!` cuando: usados ≥ 3 veces, activados hace > 30 días, y la misma categoría sigue produciendo nuevos learnings
- Un límite flexible de 100 entradas activas activa una advertencia que sugiere `4x learn prune` — las entradas nunca se eliminan automáticamente

---

## `4x mine`

Escanear todo el historial de `.4x/` en busca de señales de fallo y agregarlas en un pool de candidatos en `.4x/candidates.json`. A diferencia del auto-descubrimiento (que solo se activa en un único deep-review PASS y analiza marcadores `[NEW-FEATURE]`), el miner barre **todos** los features para obtener los datos de fallo más densos: escalaciones, features atascados y fallos de revisión recurrentes.

El miner es un escaneo puro de capa CLI/protocol — nunca llama a un LLM y nunca crea features. Solo produce candidatos; si un candidato se promueve a un feature real lo decide más tarde el gate F097.

```
4x mine                          # escanear y escribir .4x/candidates.json
4x mine --dry-run                # imprimir resumen sin escribir
4x mine --min-occurrences 5      # elevar el umbral de patrón de fallo (por defecto 3)
4x mine --output path.json       # escribir en una ruta personalizada
```

| Bandera | Por defecto | Descripción |
|---|---|---|
| `--min-occurrences` | `3` | Cantidad de features distintos que un problema de revisión recurrente debe alcanzar para convertirse en candidato |
| `--output` | `.4x/candidates.json` | Ruta de salida del pool de candidatos |
| `--dry-run` | `false` | Solo imprimir el resumen, no escribir nada |

Tres escáneres alimentan el pool, cada uno etiquetando candidatos con un `source` para trazabilidad:

- **escalation** — lee el `escalation.json` de cada ronda (`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`)
- **stuck** — features atascados en `needs-attention` / `abandoned` / `blocked`, con el motivo de bloqueo extraído de `state.json` o la escalación más reciente
- **fail-pattern** — problemas de revisión / deep-review FAIL que recurren en `>= --min-occurrences` features distintos (agrupados por similitud Jaccard); cada cluster también emite un learning candidato que sugiere una lista de verificación de revisión

El escaneo es de mejor esfuerzo: un único feature corrupto solo registra una advertencia y nunca aborta el resto. Los candidatos se deduplicarán (Jaccard) contra los features existentes, el `candidates.json` anterior y entre sí.

---

## `4x config`

Gestionar la configuración a nivel de usuario (`~/.4x/settings.json`).

```
4x config list          # mostrar toda la configuración del usuario
4x config get <key>     # obtener un valor
4x config set <key> <value>  # establecer un valor
```

Las claves son rutas con puntos. Formas soportadas:

| Clave | Ejemplo | Descripción |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | Locale de UI / prompts |
| `theme` | `4x config set theme dark` | Tema del dashboard |
| `default_runner` | `4x config set default_runner claude` | Plugin runner predeterminado |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | `command`/`model`/`tty`/`stdin`/`quiet` por runner |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` por rol |

`role.deep-reviewer.parallel_reviewers` controla cuántos sub-revisores paralelos lanza el deep review (`1` = modo single-agent como fallback); `role.deep-reviewer.angles_per_reviewer` fija la cantidad de ángulos por grupo (dejar sin configurar para balanceo automático). Ver [Conceptos -> Deep Review paralelo](concepts.md).

---

## `4x sync`

Sincronizar archivos de plugins embebidos al proyecto.

```
4x sync [--dry-run]
```

| Bandera | Descripción |
|---|---|
| `--dry-run` | Reportar diferencias sin escribir archivos |

Reporta cada archivo como creado, actualizado o vigente.

---

## `4x batch`

Operaciones batch para múltiples features.

### `4x batch plan`

Generar un plan de ejecución con reconocimiento de dependencias.

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| Bandera | Predeterminado | Descripción |
|---|---|---|
| `--dry-run` | `false` | Imprimir el plan sin escribir archivo |
| `--max-chain` | `4` | Longitud máxima de cadena por cluster |

Escribe `.4x/batch-plan.json`.

### `4x batch next`

Mostrar el próximo feature elegible para ejecutar (basado en el plan y estado actual).

```
4x batch next [--json]
```

| Bandera | Predeterminado | Descripción |
|---|---|---|
| `--json` | `false` | Salida en formato JSON con frontera de subtareas |

Sin `--json`, imprime el ID del feature como texto plano (retrocompatible). Con `--json`, produce un objeto JSON que incluye `subtaskFrontier` — las subtareas cuyas dependencias están todas completadas. Devuelve `null` en modo JSON cuando no quedan features elegibles.

### `4x batch run`

Ejecutar features elegibles secuencialmente en orden de dependencias.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| Bandera | Predeterminado | Descripción |
|---|---|---|
| `--runner` | predeterminado en config | Nombre del plugin runner |
| `--max-rounds` | `5` | Rondas máximas por feature |
| `--timeout` | `3600` | Timeout por fase en segundos |
| `--no-auto-merge` | `false` | Dejar cada feature completado en `pending-review` en lugar de fusionar automáticamente a main |

Verifica el archivo `.4x/batch-stop` entre features para detención controlada.

Cuando la ejecución termina — ya sea normalmente, por detención, por interrupción (`SIGTERM`/`SIGINT`) o por crash — escribe un `.4x/batch-report.json` resumiendo la ejecución (`outcome`, conteos de completados/fallidos/restantes, runner, duración, y el estado final de cada feature). Ver [Modo batch -> Reporte de ejecución](batch.md#reporte-de-ejecucion).

Por defecto, después de que un feature se completa (alcanza `pending-review`), el batch fusiona automáticamente el branch del worktree de vuelta a main para que el siguiente feature cree su branch desde el main actualizado — permitiendo batches continuos desatendidos. En caso de conflicto de merge, el batch se pausa de forma controlada, dejando el feature en `pending-review` y el worktree intacto, y escribe un archivo señal `.4x/batch-conflict.json` (feature, repo en conflicto, archivos) para que el [dashboard](dashboard.md) pueda mostrar el conflicto; resuelve el conflicto, ejecuta `4x merge <id>`, luego re-ejecuta `4x batch run` para continuar. El archivo señal de conflicto se limpia al inicio de cada ejecución. Los errores de merge que no son conflictos imprimen una advertencia y el batch continúa con el siguiente feature. Usa `--no-auto-merge` para restaurar el comportamiento anterior (los features se detienen en `pending-review` para revisión manual).

Si `isolation: "worktree"` está configurado, cada feature se ejecuta en su propio worktree aislado. En modo multi-repo, cada feature obtiene un worktree compuesto (`.worktrees/4x/<feature-id>/`) con subdirectorios por repo, y los commits se realizan por ronda (no se difieren hasta la finalización). Los repos hub (de la configuración `hub_repos` o `workspace.repos[*].hub: true`) se excluyen del agrupamiento por repos compartidos para permitir ejecución paralela.

### `4x batch stop`

Señalizar al batch en ejecución que se detenga después de que el feature actual termine.

```
4x batch stop
```

Crea un archivo señal `.4x/batch-stop`.

---

## `4x live [path...]`

Iniciar el servidor del dashboard 4x Live.

```
4x live [path...] [flags]
```

| Bandera | Corta | Predeterminado | Descripción |
|---|---|---|---|
| `--port` | `-p` | `4567` | Puerto del servidor |
| `--web` | `-w` | `false` | Abrir en el navegador |
| `--app` | `-a` | `false` | Abrir la app nativa de macOS |

Sin rutas, carga proyectos recientes desde `~/.4x/recent-projects.json` (LRU, máximo 20). Con rutas, abre cada una como una pestaña de proyecto.

---

## `4x mcp`

Iniciar el servidor Model Context Protocol (MCP).

```
4x mcp
```

Inicia el servidor MCP stdio de 4x para exponer los comandos del CLI de 4x como herramientas MCP a clientes LLM (ej., Claude Code, Cursor).
