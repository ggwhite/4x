# Consejos de uso y buenas prácticas

## Aviso sobre consumo de tokens

4x consume **significativamente más tokens que un solo agente**. Cada feature pasa por al menos 6 roles (Designer -> Coder -> Reviewer -> Tester -> Deep-Reviewer -> Acceptor), cada uno siendo una llamada independiente al LLM. Si Review o Test fallan, el costo en tokens aumenta significativamente por cada ronda de reintento.

Estimación aproximada por feature:

| Escenario | Aprox. llamadas al LLM | Notas |
|---|---|---|
| Aprobado al primer intento (mejor caso) | 7 | Designer + Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| Mejor caso (sin deep_model configurado) | 5 | Designer + Coder + Reviewer + Tester + Acceptor (Deep-Review omitido) |
| Review rechaza una vez | 12 | Ronda adicional de Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| Llega al máximo de 5 rondas | ~27 | Cada ronda = Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |

**Consejos para ahorrar tokens:**
- Para tareas simples, reduce `--max-rounds` (`--max-rounds 2`)
- Para tareas simples, usa modelos de nivel sonnet para todo (5-10x más barato)
- Usa `--dry-run` primero para confirmar la calidad de los prompts antes de comprometerte con una ejecución real
- Escribe descripciones claras de features para reducir escalaciones y reintentos
- El ciclo se detiene automáticamente tras 3 rondas consecutivas sin progreso — no quemará tokens hasta max-rounds innecesariamente

---

## Flujo de trabajo real (con agente de IA)

Así es como el autor realmente usa 4x en el día a día — no con comandos CLI crudos, sino con un ciclo asistido por IA donde permaneces en la misma conversación durante todo el proceso.

### 1. Crear feature

Pide al agente de IA que cree un feature por ti:

```
> 4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

### 2. Brainstorming — Spec y Plan

Antes de ejecutar el ciclo, pide al agente que haga brainstorming del diseño:

```
> brainstorm F001
```

El agente usa la habilidad de brainstorming para explorar requisitos, trade-offs y casos extremos contigo. Una vez alineados, produce dos artefactos:

- `docs/design/F001-add-redis-cache-for-or-spec.md` — design spec
- `docs/design/F001-add-redis-cache-for-or-plan.md` — plan de implementación

Estos archivos siguen la convención de nomenclatura declarada en `CLAUDE.md` bajo **Docs Routing**: `docs/design/{feature-id}-spec.md` y `docs/design/{feature-id}-plan.md`.

La spec se convierte en la referencia de entrada del Designer — una spec bien pensada en el brainstorming significa que el Designer produce mejores task briefs, lo que significa menos rechazos de review y menos rondas de reintento.

### 3. Ejecutar el ciclo

```bash
4x run F001 --runner claude
```

Abre el dashboard en otra terminal para observar el progreso:

```bash
4x live -w
```

### 4. Code review con IA

Cuando el ciclo termina (`pending-review`), pide a tu agente de IA que revise el diff:

```
> help me review the diff on branch 4x/F001-add-redis-cache-for-or
```

El agente lee `final-report.md`, compara el diff del branch contra main y señala problemas. Corrige lo necesario — ya sea manualmente o pidiéndole al agente.

### 5. Merge y limpieza

Cuando estés satisfecho, pide al agente que fusione y limpie:

```
> merge it and clean up the worktree
```

El agente ejecuta:
```bash
4x done F001
```

`4x done` automáticamente fusiona el branch, elimina el worktree y borra el branch. Si hay conflictos de merge, se te pedirá resolverlos manualmente y luego ejecutar `4x merge F001`.

### 6. Marcar como terminado en el dashboard

Abre el dashboard (`4x live -w`) y haz clic en **Mark Done** en la tarjeta del feature. Esto es intencionalmente una acción humana — el ciclo de IA nunca auto-completa un feature.

### Por qué funciona

- **Brainstorming antes de codificar** — la spec fundamenta todo el ciclo; la ambigüedad se resuelve por adelantado, no en medio de la implementación
- **Permaneces en una sola conversación** — sin cambiar contexto entre terminales y herramientas
- **El agente de IA ya tiene contexto completo** del brainstorming y la ejecución del feature, por lo que su review es informada
- **Mark Done es manual** — tú eres el guardián final, no la IA

### Qué es 4x (y qué no es)

4x es un **orquestador de flujo de trabajo** — ejecuta los roles Designer, Coder, Reviewer y Tester en secuencia y gestiona la máquina de estados entre ellos. No reemplaza tu criterio.

En la práctica, el ciclo maneja bien el camino feliz: features sencillos con specs claras generalmente pasan en 1-2 rondas. Pero el desarrollo del mundo real es desordenado:

- **El Coder puede malinterpretar la spec** — el Reviewer lo detecta, pero la corrección en la siguiente ronda puede seguir fallando. Después de 2-3 rondas fallidas, es más rápido intervenir tú mismo o pedirle a tu agente de IA que corrija el problema específico directamente.
- **Los fallos de pruebas pueden ser específicos del entorno** — el Tester escribe pruebas basadas en la spec, pero si tu proyecto tiene peculiaridades (setup de pruebas personalizado, CI inestable, restricciones legacy), las pruebas pueden fallar por razones que la IA no puede diagnosticar. Necesitarás depurar esto tú mismo.
- **Los casos extremos aparecen después del ciclo** — 4x cubre lo que la spec describe. Los casos extremos de lógica de negocio, condiciones de carrera o problemas de integración a menudo solo aparecen durante la revisión manual o en producción.
- **Los refactors complejos pueden necesitar guía humana** — cuando un feature toca muchos archivos o requiere entender convenciones implícitas, el Coder puede producir código correcto pero subóptimo. Un empujón humano rápido ("usa el helper existente en `pkg/util`") ahorra múltiples rondas de reintento.

**El modelo mental correcto**: 4x te da un primer borrador sólido con cobertura de pruebas y retroalimentación de review. Piensa en él como un desarrollador junior capaz que sigue instrucciones con precisión pero a veces necesita dirección. El ahorro de tiempo viene de no escribir la implementación inicial tú mismo — no de eliminarte del proceso por completo.

### Personalizar roles por proyecto

4x solo maneja transiciones de estado y cambio de roles — no sabe cómo tu proyecto debe construirse, probarse o revisarse. Ese conocimiento vive en la configuración de tu proyecto.

Cada rol lee de `.4x/settings.json` del proyecto para entender qué hacer. Cuanto más contexto proporciones, mejor será el resultado:

```json
{
  "project": {
    "name": "my-api",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["golangci-lint run"],
    "rules": ["all exported functions must have GoDoc comments"]
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": {
      "model": "sonnet",
      "instructions": ["always use dependency injection via constructors"]
    },
    "reviewer": {
      "model": "sonnet",
      "deep_model": "opus",
      "instructions": ["check for SQL injection in all query builders"]
    },
    "tester": {
      "model": "sonnet",
      "instructions": ["use testcontainers for integration tests, not mocks"]
    }
  }
}
```

Campos clave:

| Campo | Efecto |
|---|---|
| `project.build/test/lint` | El Coder los ejecuta después de los cambios; el Tester usa `test` para verificación |
| `project.rules` | Se inyectan en cada rol como restricciones estrictas |
| `roles.*.instructions` | Guía específica por rol — en qué enfocarse, qué evitar |
| `roles.*.includes` | Archivos adicionales a leer (ej., `["docs/api-conventions.md"]`) |

Sin estos, los roles recurren a un comportamiento genérico. Con ellos, el Designer escribe specs que coinciden con tu arquitectura, el Coder sigue tus convenciones, el Reviewer detecta los problemas específicos de tu proyecto y el Tester escribe pruebas que realmente funcionan en tu entorno.

Ver [Configuración](configuration.md) para la referencia completa.

---

## Flujo de trabajo completo (solo CLI)

El mismo flujo que arriba, pero usando comandos CLI directamente — útil cuando no estás en una sesión con agente de IA.

### Paso 1: Crear la tarea

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

Opcionalmente edita `.4x/features/F001-add-redis-cache-for-or.yaml` para completar los campos description, priority, depends, repos, etc.

### Paso 2: Ejecutar el ciclo

```bash
# Recomendado: dry run primero para verificar los prompts
4x run F001 --dry-run

# Ejecutar de verdad
4x run F001 --runner claude
```

Abre el dashboard en otra terminal para monitoreo en tiempo real:

```bash
4x live -w
```

### Paso 3: Ciclo completo -> pending-review

Cuando el ciclo termina, el feature se queda en `pending-review` — esto es intencional. La IA terminó, pero necesita tu revisión.

```bash
4x status F001
# Phase: pending-review
```

### Paso 4: Revisión humana

Revisa los resultados producidos por la IA:

```bash
# Leer el reporte final
cat .4x/F001/final-report.md

# Leer el plan de commits
cat .4x/F001/commit-plan.md

# Revisar el diff del código
git diff                          # modo sin worktree
git diff main...4x/F001-add-redis  # modo worktree
```

¿No satisfecho? Puedes reenviarlo:

```bash
# Re-ejecutar review + test después de ediciones manuales
4x transition F001 --to reviewing
4x run F001

# O comenzar desde el diseño
4x transition F001 --to designing
4x run F001
```

### Paso 5: Merge y limpieza

**Modo sin worktree** (los cambios están en el working tree):

```bash
# Marcar como terminado
4x done F001

# Comitear siguiendo el plan de commits
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Modo worktree** (los cambios están en un branch aislado):

```bash
# Marcar como terminado — automáticamente fusiona, elimina worktree y borra branch
4x done F001
```

> Si hay conflictos de merge, `4x done` imprimirá un mensaje pidiéndote resolverlos manualmente, luego ejecutar `4x merge F001` para completar el merge y la limpieza.

### Resumen del flujo

```
4x new "..."                     # crear tarea
    ↓
4x run F001 --runner claude      # IA ejecuta Design→Code→Review→Test→Deep-Review→Accept
    ↓
pending-review                   # esperando tu revisión
    ↓
review final-report / diff       # inspeccionar los resultados
    ↓
4x done F001                     # marcar como terminado + auto merge/limpieza
```

---

## Escribir buenas descripciones de features

La descripción del feature es la única entrada del Designer — cuanto más clara sea, mejor será la spec.

```bash
# Malo: demasiado vago, el Designer llenará los vacíos con suposiciones
4x new "improve performance"

# Bueno: objetivo específico, límites, criterios de aceptación
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

Incluye en tu descripción:
- **Qué hacer** (feature o cambio específico)
- **Por qué** (motivación de negocio o descripción del problema)
- **Límites** (qué NO tocar, restricciones conocidas)
- **Criterios de aceptación** (definición cuantificable de éxito)

## Granularidad de features

Un feature = un cambio entregable de forma independiente. Demasiado grande y el Coder se pierde, el Reviewer no detecta problemas y las pruebas se vuelven poco confiables.

| Granularidad | Adecuado | No adecuado |
|---|---|---|
| Un endpoint de API | OK | — |
| Un refactor (renombrar, extraer interfaz) | OK | — |
| Un bug fix | OK | — |
| Un módulo completo desde cero | — | Dividir en múltiples features + depends |
| Feature grande que abarca 3 repos | — | Un feature por repo, conectados con depends |

Usa `depends` para descomponer tareas grandes:

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## Dry run primero

Después de crear un feature nuevo o modificar la configuración, usa `--dry-run` para verificar que los prompts se vean bien:

```bash
4x run F001 --dry-run
```

Esto imprime el prompt completo para todos los roles sin llamar a ningún LLM. Verifica:
- ¿Tiene el Designer suficiente contexto?
- ¿Se inyectan correctamente las reglas de tu proyecto?
- ¿Es correcto el locale?

## Selección de modelos

| Rol | Recomendación | Razón |
|---|---|---|
| Designer | opus o equivalente | Necesita comprensión profunda para analizar requisitos y diseñar arquitectura |
| Coder | sonnet o equivalente | Alto volumen de salida, no necesita el razonamiento más fuerte |
| Reviewer (checklist) | sonnet | Verificación basada en reglas, la velocidad importa |
| Reviewer (adversarial) | opus | Necesita razonamiento profundo para encontrar bugs ocultos |
| Tester | sonnet | Escribir y ejecutar pruebas, no necesita el razonamiento más fuerte |
| Acceptor | sonnet | Verificación final contra la spec, nivel similar al reviewer |

Configuración:

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "sonnet" }
  }
}
```

Para proyectos simples (bug fixes pequeños, refactors menores), usar sonnet para todo está bien y es mucho más económico.

## Ajuste de rondas máximas

El predeterminado de 5 rondas funciona para la mayoría de los casos. Ajusta según la complejidad del feature:

| Escenario | Rondas recomendadas |
|---|---|
| Bug fix simple, cambio menor | 2-3 |
| Desarrollo de funcionalidad típica | 5 (predeterminado) |
| Feature complejo que cruza módulos | 7-10 |

```bash
4x run F001 --max-rounds 3   # tarea simple
4x run F001 --max-rounds 8   # tarea compleja
```

Nota: el ciclo se detiene automáticamente tras 3 rondas consecutivas sin progreso (no necesita llegar al max-rounds).

## Manejar fallos de review

Los fallos de review (veredicto FAIL o hallazgos CRITICAL) automáticamente envían el código de vuelta al Coder — sin intervención manual necesaria. Pero si sigue fallando:

1. **Lee review-report.md** — en `.4x/run/{feature-id}/rounds/round-{N}/review-report.md`
2. **Lee coder-report.md** — ¿entendió el Coder el problema?
3. **Considera ajustar**:
   - Descripción del feature demasiado vaga -> reescríbela, re-ejecuta desde Designer
   - Reviewer demasiado estricto -> relaja reglas específicas en `roles.reviewer.instructions`
   - Problema genuinamente difícil -> intervén manualmente, luego usa `4x transition` para avanzar

## Manejar escalaciones

Cuando el Coder o Tester encuentra que la spec no coincide con la realidad, escala automáticamente de vuelta al Designer. Escenarios comunes:

- El esquema de BD no coincide con la spec (`spec-mismatch`)
- Los criterios de aceptación son incorrectos (`criteria-wrong`)
- El alcance del feature necesita ajuste (`scope-change`)

Estas escalaciones se envían de vuelta al Designer, quien rediseña la spec. Las escalaciones se registran en `.4x/run/{feature-id}/rounds/round-{N}/escalation.json`.

Nota: las escalaciones `blocker` (ej., dependencia externa faltante) van directamente a `needs-attention` y requieren intervención manual — no se envían de vuelta al Designer.

Si el Designer tampoco puede resolverlo (generalmente por falta de contexto), el ciclo se detiene en `needs-attention`. Se necesita intervención manual:

```bash
# Verificar estado
4x status F001

# Corregir manualmente la spec o el codebase
vim .4x/F001/task-brief.md

# Empujar de vuelta a coding
4x transition F001 --to coding
```

## Reanudar un feature interrumpido

4x es basado en archivos — si la sesión se corta o la máquina se reinicia, todo el estado está en `.4x/`. Simplemente re-ejecuta:

```bash
4x run F001 --runner claude
```

Retoma desde la última fase y ronda, no desde el principio.

## Aislamiento con worktree

Para ejecutar múltiples features simultáneamente o aislar los cambios de la IA de tu working tree, habilita el modo worktree:

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

Qué sucede:
- Antes de crear la rama, si la rama actual de cada repo tiene configurado un upstream tracking branch, se hace fetch y fast-forward a esa rama — es un no-op si ya estás al día, y también un no-op (con una advertencia) si tu rama local ha divergido del remoto, así que nunca sobrescribe commits locales sin push. Si tu rama local está adelantada al remoto (commits sin push, sin divergencia) también se muestra una advertencia — el worktree se crea igualmente desde tu HEAD local, pero te avisa de los commits que aún no se han subido
- Cada feature se ejecuta en `.worktrees/4x/{feature-id}/` con su propio directorio de trabajo
- Se crea automáticamente un branch `4x/{feature-id}`
- Al completarse, el CLI muestra instrucciones de merge

```bash
# Al completarse, fusionar y limpiar automáticamente
4x done F001
# En caso de conflicto de merge, resolver manualmente y ejecutar: 4x merge F001
```

## Escenarios de uso del dashboard

```bash
# Ejecutar un feature mientras observas el dashboard
4x live -w &
4x run F001 --runner claude

# Iniciar features directamente desde la UI del dashboard
# POST /api/run vía la interfaz web

# Monitoreo multi-proyecto
4x live /path/to/project-a /path/to/project-b -w
```

## Configuración de locale

Establece el idioma para las respuestas de la IA:

```bash
4x config set locale zh-TW
```

Si no se configura, se infiere automáticamente de la variable de entorno `LANG`.

## Solución de problemas

### Feature estancado en needs-attention

Falta un artefacto requerido para la fase actual (ej., el Designer no produjo task-brief.md).

```bash
4x status F001          # ver qué falta
4x check F001           # ejecutar verificación completa
```

Corrige manualmente o re-ejecuta la fase:

```bash
4x transition F001 --to designing
4x run F001
```

### Feature estancado en blocked

Generalmente causado por código de salida 1 del runner (fallo leve). Revisa los logs:

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

Después de corregir, empuja de vuelta:

```bash
4x transition F001 --to coding
4x run F001
```

### Bloqueado por compuerta de dependencias

```
blocked: F001-user-model is not done (status: coding)
```

Completa la dependencia primero, o márcala como terminada manualmente:

```bash
4x done F001
4x run F002
```

## Integración de gstack Browse para testing E2E

[gstack](https://github.com/garrytan/gstack) proporciona un daemon de navegador headless persistente que puede acelerar el testing e2e basado en Playwright en 4x. En lugar de arrancar Chromium en frío cada ronda de tests (~3-5s), el daemon mantiene el navegador en ejecución y preserva el estado de sesión entre rondas.

Esto es **opcional** — el perfil de test `web` integrado de 4x funciona sin gstack. El daemon es más útil cuando:

- Tu proyecto requiere login (la persistencia de sesión evita re-autenticarse en cada ronda)
- Ejecutas múltiples features en paralelo (todas comparten una misma instancia del navegador)
- Quieres tiempos de respuesta del navegador inferiores a 200ms en lugar de los delays del arranque en frío

### Configuración

1. Instala gstack como skill de Claude Code:

```bash
git clone --depth 1 https://github.com/garrytan/gstack.git ~/.claude/skills/gstack
cd ~/.claude/skills/gstack && ./setup
```

2. Inicia el daemon de browse (se ejecuta en segundo plano):

```bash
# En Claude Code
/browse-open http://localhost:4567
```

O inícialo manualmente:

```bash
cd ~/.claude/skills/gstack && bun run browse/src/server.ts
```

El daemon elige un puerto aleatorio y escribe la información de conexión en `.gstack/browse.json`.

### Configurar 4x para usar gstack browse

Sobreescribe el perfil de test `web` integrado en `.4x/settings.json`:

```json
{
  "test_profiles": {
    "web": {
      "include": "docs/test-profiles/gstack-web.md"
    }
  }
}
```

Crea `docs/test-profiles/gstack-web.md`:

```markdown
Web UI E2E Testing with gstack Browse:

- Use gstack browse daemon instead of launching a standalone Playwright instance
- Read connection info from .gstack/browse.json (port + auth token)
- Send commands via HTTP POST to the daemon:
  - `POST /command` with `{"command": "goto", "args": ["http://localhost:4567"]}`
  - `POST /command` with `{"command": "snapshot"}` to get the accessibility tree with @e refs
  - `POST /command` with `{"command": "click", "args": ["@e5"]}` to interact with elements
  - `POST /command` with `{"command": "screenshot"}` to capture evidence
- Include Bearer token from browse.json in all requests
- Save screenshots to the configured screenshot_dir
- Each AC item must have at least one screenshot as evidence
- Do NOT launch a separate Chromium instance — use the running daemon
- If the daemon is not running, fall back to standard Playwright (npx playwright test)
```

### Ejemplo: test-strategy.yaml con gstack

```yaml
web: true
api: false
coder_only: false
profiles:
  - web
verify_commands:
  - "make build"
  - "make test"
```

Cuando el Designer etiqueta `profiles: [web]` y tienes la sobreescritura de `test_profiles.web` apuntando a gstack, el Tester recibe automáticamente las instrucciones específicas de gstack inyectadas en su prompt.

### Con proyectos que requieren login

Para proyectos que requieren autenticación (por ejemplo, paneles de administración), inicia sesión una vez a través de gstack antes de comenzar la ejecución de 4x:

```bash
# Abre la página de login en el daemon de gstack
/browse-open https://your-app.example.com/login

# Inicia sesión manualmente o mediante comandos fill de gstack
# Las cookies de sesión persisten en todas las rondas de test de 4x posteriores
```

El Tester puede entonces omitir el paso de login por completo — el navegador del daemon ya tiene una sesión válida.

### Sin gstack

Si no usas gstack, el perfil `web` integrado funciona de forma inmediata:

- El Tester lanza una instancia aislada de Playwright por cada ronda de test
- Crea un workspace temporal, arranca `4x live` en un puerto aleatorio
- Ejecuta los tests, toma capturas de pantalla y limpia
- No hay estado persistente entre rondas (cada ronda comienza desde cero)

Consulta [Perfiles de Test](concepts.md#test-profiles) para más detalles sobre cómo sobreescribir perfiles.

---

## Enseña a tu AI Agent 4x (una sola vez)

Por defecto, cada nueva conversación de AI vuelve a leer la documentación de 4x desde cero. Puedes eliminar esto añadiendo un **archivo de instrucciones global** para que tu agente ya conozca los comandos de 4x, la estructura de directorios y los contratos de rol antes de que comience la conversación.

### Claude Code

Crea `~/.claude/rules/4x.md` con la referencia rápida de 4x (ver ejemplo a continuación). Los archivos en `~/.claude/rules/` se cargan automáticamente en cada sesión.

### Gemini CLI

Crea `~/.gemini/instructions/4x.md` con el mismo contenido.

### Codex

Añade las instrucciones de 4x a tu `AGENTS.md` global.

### Ejemplo: Referencia Rápida de 4x para Reglas Globales

Copia [`docs/reference/4x-agent-rules.md`](../../reference/4x-agent-rules.md) al directorio de reglas globales de tu agente. Contiene:

- Todos los comandos CLI con las flags más comunes
- Estructura del directorio `.4x/`
- Contratos de rol (lecturas / escrituras / restricciones)
- Transiciones de la máquina de estados
- Runners soportados
