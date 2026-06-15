# Modo batch

Ejecutar múltiples features en orden con reconocimiento de dependencias.

## Flujo de trabajo

```bash
# 1. Generar plan de ejecución
4x batch plan

# 2. Ver qué sigue
4x batch next

# 3. Ejecutar todos los features elegibles
4x batch run --runner claude

# 4. Detención controlada (termina el feature actual)
4x batch stop
```

## Planificación

`4x batch plan` analiza todos los features no terminados (incluyendo `abandoned` y `ready-for-review`) y genera `.4x/batch-plan.json`:

1. **DAG de dependencias** — construye un grafo dirigido a partir de los campos `depends` de los features
2. **Detección de ciclos** — produce error si existen dependencias circulares
3. **Agrupación Union-Find** — agrupa features que comparten repositorios no-hub (los repos hub definidos vía configuración `hub_repos` o `workspace.repos[*].hub: true` se excluyen del agrupamiento)
4. **Ordenamiento topológico** — ordena features dentro de cada cluster. Cuando varios features quedan elegibles al mismo tiempo (sin dependencias pendientes), se ordenan por `priority` (número menor = mayor prioridad; features sin prioridad van al final). Empates en prioridad se desempatan por ID de feature para un orden estable y determinístico.
5. **Programación en cadena** — divide cadenas de dependencias largas (longitud máxima configurable con `--max-chain`)

```bash
# Vista previa del plan
4x batch plan --dry-run

# Limitar longitud de cadena
4x batch plan --max-chain 3
```

Ejemplo de salida:

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## Ejecución

`4x batch run` ejecuta features secuencialmente en orden de dependencias:

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- `--runner` es opcional; cuando se omite usa el runner predeterminado de la configuración del workspace
- Usa la estrategia de commits `"never"` (sin aislamiento) o `"per-round"` (aislamiento con worktree — comitea automáticamente cada ronda dentro del worktree del feature)
- Verifica el archivo `.4x/batch-stop` entre features
- Omite features cuyas dependencias no están completas (una dependencia cuenta como completa cuando está `done`, `abandoned` o `ready-for-review` — ver `feature.BatchCompleted`)
- Antes de ejecutar cada feature, las dependencias se re-verifican en tiempo de ejecución; si no se cumplen, el feature se marca como `blocked` y se omite
- Un feature que falla (llega a `needs-attention`, `blocked` o permanece en `in-progress`) dos veces se omite para el resto de la ejecución batch
- Reporta progreso después de cada feature

Cuando un feature alcanza `ready-for-review`, se fusiona automáticamente de vuelta a main y se marca como `done`. El worktree del siguiente feature comienza desde el main actualizado. Usa `--no-auto-merge` para deshabilitar esto — los features se detienen en `ready-for-review` en lugar de fusionarse. En caso de conflicto de merge, el batch se pausa (ver [Conflictos de merge](#conflictos-de-merge)). En errores que no son conflictos, el batch registra una advertencia y continúa con el siguiente feature.

```bash
# Ejecutar sin auto-merge
4x batch run --runner claude --no-auto-merge
```

> **Nota:** `batch run` siempre regenera el plan desde cero internamente (ignorando cualquier `batch-plan.json` existente). También usa un filtro más estricto que `batch plan` — los features que están `done`, `abandoned` o `ready-for-review` se excluyen de la ejecución. `batch plan` es útil para ver la programación en vista previa o alimentar `batch next`, pero no es un prerequisito para `batch run`.

## Detención

```bash
4x batch stop
```

Crea un archivo señal `.4x/batch-stop`. El batch termina el feature actual y luego sale de forma controlada.

## Conflictos de merge

Cuando el auto-merge encuentra un conflicto, el batch se pausa y escribe `.4x/batch-conflict.json` registrando el feature, el repo en conflicto (modo multi-repo) y los archivos afectados. El worktree se preserva para que puedas resolver el conflicto. El archivo señal permite al [dashboard](dashboard.md) mostrar el conflicto y ofrecer una acción de **Continuar batch** — internamente limpia el archivo señal y reinicia `4x batch run`. Desde el CLI, resuelve los archivos, ejecuta `4x merge <id>`, luego re-ejecuta `4x batch run` para continuar. El archivo de conflicto se limpia automáticamente al inicio de cada ejecución batch.

## Reporte de ejecución

Cada ejecución batch escribe `.4x/batch-report.json` cuando termina — ya sea que haya finalizado normalmente, se haya detenido, interrumpido o crasheado. El reporte registra estadísticas generales (total / completados / fallidos / restantes), el runner, la duración total y el nombre de cada feature, estado final, duración, conteo de rondas y razón de detención.

El campo `outcome` captura cómo terminó la ejecución:

- `completed` — todos los features terminaron
- `stopped` — presionaste Detener (`.4x/batch-stop`) o un conflicto de auto-merge pausó la ejecución
- `interrupted` — el proceso batch recibió `SIGTERM`/`SIGINT`; el reporte registra el feature que estaba en ejecución
- `crashed` — el proceso batch hizo panic; el reporte es best-effort e incluye `panicMessage`

El [dashboard](dashboard.md) lee este archivo cuando no hay batch en ejecución y muestra una tarjeta resumen de "último reporte batch" que se expande a detalle por feature. El reporte se escribe solo después de que la ejecución se detiene, nunca dentro del ciclo de ejecución por feature, por lo que no agrega overhead al rendimiento del batch.

## Verificar progreso

```bash
# Ver qué feature sigue (imprime el ID del feature)
4x batch next

# Salida JSON con información de frontera de subtareas
4x batch next --json

# Resumen de todos los features
4x status
```

Con `--json`, la salida incluye la frontera de dependencias de subtareas — el conjunto de subtareas cuyas dependencias están todas completadas y están listas para trabajar:

```json
{
  "featureId": "F044-subtask-frontier",
  "slot": 0,
  "subtaskFrontier": ["parse-depends", "build-dag"]
}
```

Retorna `null` cuando no quedan features elegibles.
