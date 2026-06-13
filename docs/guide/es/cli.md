# Referencia del CLI

Todos los argumentos de feature-id soportan coincidencia por prefijo insensible a mayúsculas. `4x run f001`, `4x run F001-user` y `4x run F001` resuelven a `F001-user-authentication-w`. Los prefijos ambiguos producen un error listando las coincidencias.

---

## `4x init`

Inicializar un workspace `.4x/` en el directorio actual.

```
4x init
```

- Detecta automáticamente el lenguaje del proyecto y los comandos de build/test/lint
- Crea `.4x/settings.json` con 4 runners predeterminados (claude, codex, gemini, agy)
- Despliega archivos de plugins embebidos en `.4x/plugins/`
- Agrega líneas `@import` a archivos del nivel raíz (CLAUDE.md, AGENTS.md, GEMINI.md, AGY.md)
- Produce error si `.4x/` ya existe

---

## `4x new <title>`

Crear un nuevo feature.

```
4x new "Feature title" [--repo <repo>...] [--json]
```

| Bandera | Descripción |
|---|---|
| `--repo` | Repositorio dentro del alcance (repetible para features multi-repo) |
| `--json` | Salida en formato JSON |

Crea `.4x/features/F{NNN}-{slug}.yaml` con estado `not-started`.

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

El ciclo ejecuta: init → designing → coding → reviewing → testing → accepting → pending-review. En fallo de review, code recibe otra pasada. En fallo de test, el ciclo re-entra en coding.

Si la función está en fase `blocked` o `needs-attention`, se recupera automáticamente a la fase de reanudación apropiada según el rol actual.

Verifica automáticamente la compuerta de dependencias — bloquea si los features dependientes no están completados.

Si `isolation: "worktree"` está configurado, ejecuta en un git worktree bajo `.worktrees/4x/<feature-id>/`.

---

## `4x status [feature-id]`

Mostrar estado de features.

```
4x status              # all features, grouped by state
4x status <feature-id> # single feature details with subtasks
4x status --pending    # filter pending-review features
4x status --json       # output as JSON
```

| Bandera | Descripción |
|---|---|
| `--pending` | Filtrar features en pending-review |
| `--json` | Salida en formato JSON |

Grupos: Running, Review, Pending, Todo, Done (done muestra máximo 5). Incluye advertencias de desvío del backlog.

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

Valida que la transición sea legal según la máquina de estados. Auto-inicializa el estado si no existe. La transición `testing → accepting` ejecuta compuertas adicionales (verify.json, test-report.md, final-report.md, commit-plan.md deben existir y la verificación debe aprobar).

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

Soporta inyección de locale (desde la configuración del usuario o la variable de entorno `LANG`), inclusión automática de documentos de planificación (`docs/design/{id}-spec.md` y `{id}-plan.md`), e includes de proyecto/rol.

---

## `4x done <feature-id>`

Marcar un feature en pending-review como terminado.

```
4x done <feature-id>
```

Solo funciona cuando el feature está en fase `pending-review`. Produce error en cualquier otra fase.

---

## `4x config`

Gestionar la configuración a nivel de usuario (`~/.4x/settings.json`).

```
4x config list          # show all user config
4x config get <key>     # get a value
4x config set <key> <value>  # set a value
```

Clave soportada actualmente: `locale`.

---

## `4x sync`

Volver a desplegar archivos de plugins embebidos en un proyecto existente.

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
4x batch next
```

### `4x batch run`

Ejecutar features elegibles secuencialmente en orden de dependencias.

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| Bandera | Predeterminado | Descripción |
|---|---|---|
| `--runner` | predeterminado en config | Nombre del plugin runner |
| `--max-rounds` | `5` | Rondas máximas por feature |
| `--timeout` | `3600` | Timeout por fase en segundos |

Verifica el archivo `.4x/batch-stop` entre features para detención controlada.

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
