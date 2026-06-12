# Runners y plugins

## ¿Qué es un runner?

Un runner es un puente entre el CLI de 4x y una herramienta de IA. El CLI genera prompts de roles y gestiona el estado; el runner envía los prompts a la IA y captura la salida.

Los runners se configuran en `.4x/settings.json` bajo la clave `runners`. El CLI invoca runners como subprocesos.

## Runners integrados

| Runner | Herramienta de IA | Modo | Estado |
|---|---|---|---|
| `claude` | Claude Code CLI | PTY (tty: true) | Disponible |
| `codex` | OpenAI Codex CLI | Stdin | Disponible |
| `gemini` | Google Gemini CLI | Argumento | Disponible |
| `agy` | Antigravity CLI | Argumento | Disponible |
| `copilot` | GitHub Copilot CLI | Argumento | Disponible (config manual) |
| `cursor` | Cursor IDE | Archivo de reglas | Disponible (config manual) |

`4x init` configura claude, codex, gemini y agy de forma predeterminada. Copilot y cursor requieren adición manual a `settings.json`.

## Archivos de plugins

Cada runner tiene archivos de instrucciones embebidos en el binario de `4x`. `4x init` los despliega en `.4x/plugins/` y agrega líneas de importación a archivos del nivel raíz:

| Runner | Archivo de plugin | Importación raíz |
|---|---|---|
| claude | `SKILL.md` + `workflow.js` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` + `workflow.js` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

Usa `4x upgrade` para volver a desplegar archivos de plugins después de actualizar el binario.

## Modelo de ejecución de runners

```
4x run F001 --runner claude
    │
    ├── Generate prompt for current role
    ├── Invoke runner subprocess with prompt
    │     claude --dangerously-skip-permissions -p "..."
    ├── Capture output to .4x/F001/logs/round-N-role.log
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

### Modo PTY

Los runners con `tty: true` (Claude Code) usan un pseudo-terminal para capturar la salida completa incluyendo secuencias de escape ANSI. Un limpiador de ANSI con estado limpia los archivos de log.

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
