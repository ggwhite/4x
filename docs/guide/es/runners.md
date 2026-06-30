# Runners y plugins

## ¿Qué es un runner?

Un runner es un puente entre el CLI de 4x y una herramienta de IA. El CLI genera prompts de roles y gestiona el estado; el runner envía los prompts a la IA y captura la salida.

Los runners se configuran en `.4x/settings.json` bajo la clave `runners`. El CLI invoca runners como subprocesos.

## Runners integrados

| Runner | Herramienta de IA | Modo | Estado |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | Disponible |
| `codex` | OpenAI Codex CLI | Stdin | Disponible |
| `gemini` | Google Gemini CLI | Argumento | Disponible |
| `agy` | Antigravity CLI | Argumento | Disponible |
| `opencode` | OpenCode CLI | Argumento | Disponible |
| `copilot` | GitHub Copilot CLI | Argumento | Disponible (config manual) |
| `cursor` | Cursor IDE | Archivo de reglas | Disponible (config manual) |

`4x init` configura claude, codex, gemini, agy y opencode de forma predeterminada. Copilot y cursor requieren adición manual a `settings.json`.

## Archivos de plugins

Cada runner tiene archivos de instrucciones embebidos en el binario de `4x`. `4x init` los despliega en `.4x/plugins/` y agrega líneas de importación a archivos del nivel raíz:

| Runner | Archivo de plugin | Importación raíz |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| opencode | `AGENTS.md` | AGENTS.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

Además, los archivos de instrucciones compartidos se despliegan en `.4x/plugins/shared/` para todos los runners:

| Archivo | Propósito |
|---|---|
| `shared/CREATOR.md` | Flujo de Feature Creator — guía a la IA para crear features con `4x new` |

Usa `4x sync` para volver a desplegar archivos de plugins después de actualizar el binario.

## Modelo de ejecución de runners

```
4x run F001 --runner claude
    │
    ├── Generate prompt for current role
    ├── Invoke runner subprocess with prompt
    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
    ├── Capture output to .4x/run/F001/logs/round-N-role.log
    ├── Check output artifacts
    └── Transition state, repeat
```

### Códigos de salida

| Código | Significado | Acción |
|---|---|---|
| 0 | Éxito | Proceder a la siguiente fase |
| 1 | Fallo leve | El feature pasa a `blocked` |
| 2 | Error grave | El ciclo se detiene, requiere atención |
| timeout | Sin respuesta dentro del límite | Tratado como fallo leve |

Cuando el ciclo de ejecución se interrumpe (por ejemplo con Ctrl+C), la cancelación de contexto se maneja como una interrupción limpia — el feature no queda en `needs-attention`. La fase en progreso se trata como incompleta, y el siguiente `4x run` reanuda desde esa fase.

### Resolución de placeholders

Los `args` de los runners pueden contener placeholders que el CLI sustituye antes de invocar el subproceso:

| Placeholder | Reemplazado por |
|---|---|
| `{prompt}` | El texto del prompt del rol, en línea como argumento |
| `{promptFile}` | Ruta a un archivo temporal que contiene el prompt |
| `{model}` | El override de modelo resuelto para este rol |

La resolución de placeholders **falla explícitamente** en lugar de pasar un placeholder literal al CLI de IA:

- `{model}` presente pero sin override de modelo resuelto → el runner produce error con `model not resolved for runner <name>` en lugar de enviar `--model {model}` (que el CLI rechazaría con un error opaco).
- `{promptFile}` pero el archivo temporal no puede crearse o escribirse (por ejemplo `/tmp` lleno) → el runner retorna el error subyacente envuelto (`runner <name>: create prompt temp file: ...`) y elimina cualquier archivo temporal parcialmente creado, en lugar de enviar la cadena literal `{promptFile}`.

Cualquier archivo temporal creado durante la resolución siempre se limpia, incluso cuando un paso posterior falla.

### Modo Stream JSON

Los runners con `output_format: "stream-json"` escriben dos archivos: un `.log` legible para el tail del dashboard y un `.stream.jsonl` raw para depuración. Claude Code usa este modo por defecto.

### Manejo de grupos de procesos no-PTY

Los runners no-PTY (modo stream-json, modo stdin, modo de argumento plano) usan un grupo de procesos independiente (`Setpgid` en Unix). Cuando el contexto de ejecución se cancela, al grupo de procesos se le envía `SIGKILL` inmediatamente — no hay periodo de gracia SIGTERM. En Windows, se aplica el comportamiento predeterminado de `exec.CommandContext`.

### Modo PTY

Los runners con `tty: true` (y que no usan `output_format: "stream-json"`) usan un pseudo-terminal para capturar la salida completa incluyendo secuencias de escape ANSI. Un limpiador de ANSI con estado limpia los archivos de log. La ruta PTY usa `exec.Command` con un observador de contexto dedicado para el apagado controlado, mientras que los runners no-PTY usan `exec.CommandContext` con cancelación a nivel de grupo de procesos (ver arriba).

El hijo PTY se ejecuta en su propia sesión/grupo de procesos. Cuando el contexto de ejecución se cancela (por ejemplo timeout o Ctrl+C), se envía `SIGTERM` a todo el grupo de procesos, escalando a `SIGKILL` después de 5 segundos si no ha salido — para que ningún hijo huérfano sobreviva a la ejecución.

### Modo stdin

Los runners con `stdin: true` (Codex) reciben el prompt vía entrada estándar en lugar de argumentos de línea de comandos.

## Usar diferentes modelos por rol

Configurar en `.4x/settings.json`:

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

> **Nota:** `deep_model` se configura en el rol **reviewer** (no en deep-reviewer). Si `roles.reviewer.deep_model` no está configurado, la fase `deep-reviewing` se **omite por completo** — la ejecución transiciona directamente de `testing` a `accepting`. Esto es intencional: la revisión profunda es opt-in.

También puedes mezclar runners — usa Claude para Design, Gemini para Code, etc. — ejecutando cada fase manualmente con diferentes banderas `--runner` y `4x transition` entre fases.

## Escribir un plugin

Los plugins siguen un contrato simple — leer archivos de `.4x/`, hacer el trabajo de IA, escribir los resultados de vuelta:

1. Leer `.4x/features/{id}.yaml` para conocer el feature
2. Leer `state.json` para conocer la fase actual
3. Leer las entradas específicas de la fase (task-brief.md, alcance, etc.)
4. Hacer el trabajo (llamar a tu LLM, ejecutar herramientas)
5. Escribir las salidas específicas de la fase (coder-report.md, review-report.md, etc.)
6. Salir con el código apropiado (0 = éxito, 1 = fallo leve, 2 = error grave)

No requiere SDK. No requiere dependencia en tiempo de ejecución. Solo archivos.
