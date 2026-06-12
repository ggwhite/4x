# Configuración

## Configuración del proyecto (`.4x/settings.json`)

Creada por `4x init`. Contiene metadatos del proyecto, definiciones de runners y mapeo de modelos por rol.

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus",
      "tty": true
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Sección project

| Campo | Descripción |
|---|---|
| `name` | Nombre del proyecto (detectado automáticamente del directorio) |
| `language` | Lenguaje detectado |
| `build` | Comandos de build |
| `test` | Comandos de test |
| `lint` | Comandos de lint |
| `setup` | Comandos de setup (ej., `docker-compose up -d`) |
| `docs` | Rutas de archivos de documentación para referencia del Designer |
| `rules` | Reglas específicas del proyecto inyectadas en los prompts de roles |

### Configuración de runners

| Campo | Descripción |
|---|---|
| `command` | Nombre del ejecutable |
| `args` | Argumentos. `{prompt}` y `{promptFile}` se reemplazan en tiempo de ejecución. `{model}` se reemplaza con el modelo del rol. |
| `model` | Modelo predeterminado para este runner |
| `model_map` | Mapeo de nombres de modelo de rol a nombres específicos del runner (ej: `{"opus": "claude-opus-4-5-20250514"}`). Orden de búsqueda: modelo del rol → traducción model_map → nombre original como respaldo. |
| `tty` | Usar PTY para capturar salida (necesario para herramientas CLI con salida ANSI como Claude Code) |
| `stdin` | Enviar el prompt vía stdin en lugar de argumento (usado por Codex) |
| `quiet` | Suprime la salida stdout del runner en la terminal; la salida se sigue capturando en archivos de log. |

Si `{model}` no está presente en `args`, el runner agrega automáticamente `--model <model>`.

### Configuración de roles

| Campo | Descripción |
|---|---|
| `model` | Nombre del modelo para este rol |
| `deep_model` | Modelo para la pasada de revisión adversarial (solo reviewer) |
| `instructions` | Instrucciones adicionales inyectadas en el prompt del rol |
| `includes` | Archivos a incluir en el prompt del rol |

### Otros campos de configuración

| Campo | Descripción |
|---|---|
| `hub_repos` | Repositorios compartidos (para agrupación de DAG en batch) |
| `isolation` | Establecer como `"worktree"` para ejecutar features en git worktrees |
| `max_concurrent_runs` | Ejecuciones concurrentes máximas vía el servidor del dashboard |
| `commit` | Estrategia de commits: `"per-round"` (predeterminado), `"on-done"` o `"never"` |

## Configuración del usuario (`~/.4x/settings.json`)

Preferencias globales del usuario. Gestionadas vía `4x config`.

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

### Locale

Establece el idioma para las instrucciones de los prompts de roles. Valores soportados:

| Valor | Idioma |
|---|---|
| `en` | Inglés (predeterminado) |
| `zh-TW` | Chino tradicional |
| `zh-CN` | Chino simplificado |
| `ja` | Japonés |
| `ko` | Coreano |
| `es` | Español |
| `fr` | Francés |
| `de` | Alemán |
| `pt` | Portugués |
| `ru` | Ruso |
| `vi` | Vietnamita |

El locale también se infiere de la variable de entorno `LANG` si no se establece explícitamente.
