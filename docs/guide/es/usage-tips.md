# Consejos de uso y buenas prácticas

## Aviso sobre consumo de tokens

4x consume **significativamente más tokens que un solo agente**. Cada feature pasa por al menos 4 roles (Designer → Coder → Reviewer → Tester), cada uno siendo una llamada independiente al LLM. Si Review o Test fallan y disparan una repetición, los tokens se duplican.

Estimación aproximada de consumo de tokens por feature:

| Escenario | Aprox. llamadas al LLM | Explicación |
|---|---|---|
| Aprobado al primer intento (mejor caso) | 5 | Designer + Coder + Reviewer(2 pasadas) + Tester |
| Review rechaza 1 vez | 8 | Una ronda adicional de Coder + Reviewer + Tester |
| Llega al máximo de 5 rondas | ~20 | Cada ronda es Coder + Reviewer + Tester |

**Consejos para ahorrar tokens:**
- Para tareas simples, reduce `--max-rounds` (`--max-rounds 2`)
- Para tareas simples, usa modelos de nivel sonnet para todo (5-10x más barato)
- Usa `--dry-run` primero para confirmar la calidad de los prompts y evitar desperdicio
- Escribe descripciones claras de features para reducir escalaciones y repeticiones
- El ciclo se detiene automáticamente tras 3 rondas consecutivas sin progreso, no se quemará hasta max-rounds

---

## Flujo de trabajo completo

El flujo completo desde la creación de la tarea hasta la entrega — 4x se encarga del desarrollo con IA, tú te encargas de la revisión final y el merge.

### Paso 1: Crear la tarea

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

Si es necesario, edita `.4x/features/F001-add-redis-cache-for-or.yaml` para complementar los campos description, priority, depends, repos, etc.

### Paso 2: Ejecutar el ciclo

```bash
# Recommended: dry run first to check the prompt
4x run F001 --dry-run

# Run for real
4x run F001 --runner claude
```

Puedes abrir el dashboard para observar en tiempo real:

```bash
4x live -w   # in another terminal
```

### Paso 3: Ciclo completo → pending-review

Cuando el ciclo termina, el feature se queda en `pending-review` — esto es intencional. La IA terminó, pero necesita tu revisión.

```bash
4x status F001
# Phase: pending-review
```

### Paso 4: Revisión humana

Revisa los resultados producidos por la IA:

```bash
# View the final report
cat .4x/F001/final-report.md

# View the commit plan
cat .4x/F001/commit-plan.md

# View code diff
git diff                          # non-worktree mode
git diff main...4x/F001-add-redis  # worktree mode
```

Si no estás satisfecho, puedes:

```bash
# Manually modify then re-run review + test
4x transition F001 --to reviewing
4x run F001

# Or start completely over
4x transition F001 --to designing
4x run F001
```

### Paso 5: Merge y limpieza

**Modo sin worktree** (los cambios están directamente en el working tree):

```bash
# Mark as done when satisfied
4x done F001

# Commit following commit-plan.md
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Modo worktree** (los cambios están en un branch independiente):

```bash
# Mark as done
4x done F001

# Merge to main branch
git merge 4x/F001-add-redis-cache-for-or

# Clean up worktree and branch
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### Resumen del flujo

```
4x new "..."                     # Create task
    ↓
4x run F001 --runner claude      # AI runs Design→Code→Review→Test automatically
    ↓
pending-review                   # Waiting for your review
    ↓
review final-report / diff       # You review the results
    ↓
4x done F001                     # Mark as done
    ↓
git merge + cleanup              # Merge, clean worktree/branch
```

---

## Escribir buenas descripciones de features

La descripción del feature es la única entrada del Designer — cuanto más clara sea, más precisa será la spec resultante.

```bash
# Bad: too vague, Designer will fill in the blanks with assumptions
4x new "improve performance"

# Good: clear objective, boundaries, acceptance criteria
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

Se recomienda incluir en la descripción:
- **Qué hacer** (funcionalidad o modificación concreta)
- **Por qué hacerlo** (motivación de negocio o descripción del problema)
- **Límites** (qué no tocar, restricciones conocidas)
- **Criterios de aceptación** (definición cuantificable de éxito)

## Granularidad de features

Un feature corresponde a un cambio que se puede entregar de forma independiente. Si es demasiado grande, el Coder se pierde, el Reviewer no detecta errores y las pruebas son difíciles de validar.

| Granularidad | Adecuado | No adecuado |
|---|---|---|
| Un endpoint de API | OK | — |
| Un refactor (renombrar, extraer interfaz) | OK | — |
| Un bug fix | OK | — |
| Un módulo completo desde cero | — | Dividir en múltiples features + depends |
| Una funcionalidad grande que abarca 3 repos | — | Un feature por repo, conectados con depends |

Aprovecha `depends` para desglosar tareas grandes:

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## Dry run antes de la ejecución real

La primera vez que usas un feature nuevo o después de cambiar settings, usa `--dry-run` para ver si el prompt es razonable:

```bash
4x run F001 --dry-run
```

Esto imprime los prompts completos de los cuatro roles sin llamar al LLM, permitiéndote confirmar:
- Si el Designer tiene suficiente contexto
- Si tus reglas de proyecto se inyectan correctamente
- Si el locale es correcto

## Recomendaciones de selección de modelos

| Rol | Recomendación | Razón |
|---|---|---|
| Designer | opus o equivalente | Necesita comprensión profunda de requisitos, descomposición de arquitectura |
| Coder | sonnet o equivalente | Alto volumen de salida, pero no requiere el razonamiento más fuerte |
| Reviewer (checklist) | sonnet | Verificación basada en reglas, prioridad en velocidad |
| Reviewer (adversarial) | opus | Necesita razonamiento profundo para encontrar bugs ocultos |
| Tester | sonnet | Escribir pruebas, ejecutar verificaciones, no requiere el razonamiento más fuerte |

Método de ajuste:

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

Si el proyecto es simple (bug fix pequeño, refactor menor), usar sonnet para todo también funciona y ahorra costos.

## Ajuste de rondas

El predeterminado de 5 rondas es adecuado para la mayoría de los casos. Ajusta según la complejidad del feature:

| Escenario | Rondas recomendadas |
|---|---|
| Bug fix simple, cambio menor | 2-3 |
| Desarrollo de funcionalidad general | 5 (predeterminado) |
| Funcionalidad compleja que cruza módulos | 7-10 |

```bash
4x run F001 --max-rounds 3   # simple task
4x run F001 --max-rounds 8   # complex task
```

Nota: el ciclo se detiene automáticamente tras 3 rondas consecutivas sin progreso (no necesita llegar al max-rounds).

## Manejar fallos de review

Un fallo en Review (veredicto FAIL o hallazgos CRITICAL) envía automáticamente al Coder para correcciones, sin necesidad de intervención humana. Pero si falla repetidamente:

1. **Revisa review-report.md** — en `.4x/{feature-id}/rounds/round-{N}/review-report.md`
2. **Revisa coder-report.md** — ¿el Coder entendió el problema?
3. **Considera ajustar**:
   - La descripción del feature es demasiado vaga → reescribe la descripción, vuelve a ejecutar el Designer
   - El Reviewer es demasiado estricto → relaja reglas específicas en `roles.reviewer.instructions`
   - Es realmente un problema difícil → intervención manual, luego usa `4x transition` para avanzar

## Manejar escalaciones

Cuando el Coder o Tester descubre que la spec no coincide con la realidad, escala automáticamente al Designer. Escenarios comunes:

- El esquema de DB no coincide con lo descrito en la spec (`spec-mismatch`)
- Los criterios de aceptación no son razonables (`criteria-wrong`)
- Falta una dependencia externa (`blocker`)

Las escalaciones se registran en `.4x/{feature-id}/rounds/round-{N}/escalation.json`. El Designer recibe el contenido de la escalación y produce una nueva spec.

Si el Designer tampoco puede resolverlo (generalmente por falta de contexto), el ciclo se detiene en `needs-attention`, momento en que se necesita intervención humana:

```bash
# Check status
4x status F001

# Manually fix spec or codebase
vim .4x/F001/task-brief.md

# Push back to coding to continue
4x transition F001 --to coding
```

## Reanudar un feature interrumpido

4x está basado en archivos — si la sesión se corta o la máquina se reinicia, el estado está en `.4x/`. Simplemente vuelve a ejecutar:

```bash
4x run F001 --runner claude
```

Continuará desde la última fase y ronda, sin empezar de cero.

## Aislamiento con worktree

Si ejecutas múltiples features simultáneamente, o quieres aislar las modificaciones de la IA, habilita worktree:

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

Efecto:
- Cada feature trabaja de forma independiente en `.worktrees/4x/{feature-id}/`
- Se crea automáticamente un branch `4x/{feature-id}`
- Al completarse, se muestra el comando de merge

```bash
# After completion, merge
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
```

## Cuándo usar batch

| Escenario | Usar `4x run` | Usar `4x batch run` |
|---|---|---|
| Hacer un feature | OK | — |
| Hacer múltiples features con dependencias | Hay que ordenar manualmente | Maneja el orden de dependencias automáticamente |
| Procesar el backlog durante la noche | — | OK, combinado con `batch stop` para detenerse cuando sea necesario |

La estrategia de commits del batch es fija en `"never"` — todos los cambios quedan en el working tree, y después de completarse, el humano revisa y hace commit.

## Escenarios de uso del dashboard

```bash
# Run a feature with the dashboard open, watch logs in real time
4x live -w &
4x run F001 --runner claude

# Start a feature directly from the dashboard (no terminal needed)
# POST /api/run via web UI

# Multi-project monitoring
4x live /path/to/project-a /path/to/project-b -w
```

## Configuración de locale

Haz que la IA responda en tu idioma:

```bash
4x config set locale zh-TW
```

También puedes no configurarlo — se inferirá automáticamente de la variable de entorno `LANG`.

## Solución de problemas

### Feature estancado en needs-attention

Significa que alguna fase carece de un artefacto necesario (por ejemplo, el Designer no produjo task-brief.md).

```bash
4x status F001          # see what's missing
4x check F001           # run full check
```

Complementa el archivo manualmente o vuelve a ejecutar esa fase:

```bash
4x transition F001 --to designing
4x run F001
```

### Feature estancado en blocked

Generalmente es porque el runner terminó con código de salida 1 (fallo leve). Revisa el log:

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

Después de resolver, empuja de vuelta:

```bash
4x transition F001 --to coding
4x run F001
```

### Compuerta de dependencias bloqueando

```
blocked: F001-user-model is not done (status: coding)
```

Primero completa el feature del que se depende, o márcalo manualmente:

```bash
4x done F001
4x run F002
```
