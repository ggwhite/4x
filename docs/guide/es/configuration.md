# Configuración

## Configuración del proyecto (`.4x/settings.json`)

Creada por `4x init`. Contiene metadatos del proyecto, definiciones de runners y mapeo de modelos por rol.

También puedes editar este archivo visualmente desde el **dashboard 4x Live** — haz clic en el icono de engranaje (⚙) junto al título "4x Live", o presiona `Cmd+Shift+,`. El editor soporta tanto una vista de formulario como una vista de JSON crudo, valida campos obligatorios y respalda la configuración anterior en `settings.json.bak` antes de escribir.

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
	      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
	      "model": "opus",
	      "output_format": "stream-json"
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
  "default_runner": "claude",
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
| `description` | Descripción del proyecto (opcional) |
| `docs` | Rutas de archivos de documentación para referencia del Designer |
| `rules` | Reglas específicas del proyecto inyectadas en los prompts de roles |
| `includes` | Archivos a incluir en los prompts de roles |

### Configuración de runners

| Campo | Descripción |
|---|---|
| `command` | Nombre del ejecutable |
| `args` | Argumentos. `{prompt}` y `{promptFile}` se reemplazan en tiempo de ejecución. `{model}` se reemplaza con el modelo del rol. |
| `model` | Modelo predeterminado para este runner |
| `tiers` | Mapeo de nombres de tier a nombres de modelo específicos del runner (ej: `{"opus": "claude-opus-4-5-20250514"}`). Orden de búsqueda: modelo del rol -> traducción en tiers -> nombre original como respaldo. |
| `output_format` | Con `"stream-json"`, stdout del runner se convierte en un `.log` legible y un `.stream.jsonl` raw. |
| `tty` | Usa PTY para capturar salida. Se omite cuando `output_format` es `"stream-json"`. |
| `stdin` | Enviar el prompt vía stdin en lugar de argumento (usado por Codex) |
| `quiet` | Suprime la salida stdout del runner en la terminal; la salida se sigue capturando en archivos de log. |

Si `{model}` no está presente en `args`, el runner agrega automáticamente `--model <model>`.

### Configuración de roles

| Campo | Descripción |
|---|---|
| `model` | Nombre del modelo para este rol |
| `deep_model` | Modelo para la pasada de revisión adversarial (solo reviewer) |
| `max_fix_rounds` | Iteraciones máximas de auto-reparación en la fase `deep-reviewing` (solo `deep-reviewer`; predeterminado 2). Cada iteración ejecuta un mini-coder + re-verifier focalizados; exceder el límite escala a `needs-attention`. |
| `instructions` | Instrucciones adicionales inyectadas en el prompt del rol |
| `includes` | Archivos a incluir en el prompt del rol |
| `screenshot_dir` | Ruta del directorio para capturas de pantalla del tester |
| `parallel_reviewers` | Número de sub-revisores paralelos para deep review (solo deep-reviewer; <=1 vuelve al modo agente único) |
| `angles_per_reviewer` | Ángulos de revisión por sub-revisor (solo deep-reviewer; 0 significa distribución automática uniforme) |

### Otros campos de configuración

| Campo | Descripción |
|---|---|
| `hub_repos` | Repositorios compartidos (para agrupación de DAG en batch) |
| `isolation` | Establecer como `"worktree"` para ejecutar features en git worktrees |
| `max_concurrent_runs` | Ejecuciones concurrentes máximas vía el servidor del dashboard |
| `commit` | Estrategia de commits: `"per-round"` (predeterminado), `"on-done"` o `"never"` |
| `profiles` | Perfiles de pipeline con nombres (subconjuntos de roles); ver [Perfiles](#perfiles) |
| `parallel_review_test` | Ejecutar reviewer y tester concurrentemente durante la fase de reviewing (predeterminado `false`) |
| `auto_discover_features` | Auto-crear features a partir de marcadores `[NEW-FEATURE]` en el reporte de deep review (predeterminado `false`); ver [Auto-descubrimiento de features](#auto-descubrimiento-de-features) |
| `workspace` | Configuración de workspace multi-repo (mapeo nombre de repo -> ruta) |
| `hooks` | Hooks de ciclo de vida (indexados por punto de hook, ej. post-run) |
| `health_check` | Comandos globales de verificación de salud pre-test del entorno (puede sobrescribirse por feature en test-strategy.yaml) |
| `test_profiles` | Definiciones de perfiles de pruebas personalizados o sobrescritos (indexados por nombre de perfil) |
| `max_discovered_features` | Máximo de features auto-creados por ejecución; sin configurar o `<= 0` aplica el predeterminado (`3`) |

### Auto-descubrimiento de features

Cuando `auto_discover_features` es `true`, el ciclo de ejecución analiza el reporte final de deep review (`deep-review-report.md`) después de que **aprueba** y convierte cada marcador `[NEW-FEATURE]` en un nuevo YAML de feature — capturando los problemas fuera de alcance que el deep reviewer detectó en lugar de dejarlos enterrados.

- **Punto de activación**: solo se dispara cuando el deep review final aprueba (PASS en primera pasada, o PASS después de auto-reparación). Las rondas intermedias, fallos de reviewer/tester, y rutas de FAIL/needs-attention del deep review nunca lo alcanzan.
- **Deduplicación**: cada candidato se compara (similitud por solapamiento de tokens) contra el nombre + descripción de cada feature existente, y contra los candidatos ya conservados en el mismo lote. Los candidatos similares se omiten.
- **Límite**: se crean como máximo `max_discovered_features` (predeterminado `3`) features por ejecución; el resto se registra como limitado.
- **Salida**: se escribe un resumen `discovered-features.md` bajo `.4x/<feature-id>/` listando candidatos creados / omitidos-como-duplicado / limitados, y se agrega un evento `feature-discovered` por cada feature creado.

Todo esto ocurre en la capa del CLI (parseo de texto plano + escritura de archivos, sin llamada a LLM) y nunca bloquea la transición a `accepting` — cualquier error se registra como best-effort.

### Perfiles

Un perfil selecciona qué phases se ejecutan para un feature, de modo que features simples pueden omitir el pipeline completo. Las phases no listadas se omiten — el estado avanza por la arista legal sin invocar el runner, verificar artefactos ni ejecutar guardrails. `coding` es la única phase obligatoria; un perfil que no la incluya es un error de configuración. La phase opcional `design-reviewing` solo se ejecuta cuando está incluida, y su `design-review-report.md` debe PASS antes de que comience coding.

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

Cada entrada de phase soporta sobrescrituras opcionales de `runner` y `model`:

| Campo | Descripción |
|---|---|
| `phase` | Nombre de la phase (debe ser una phase seleccionable: designing, design-reviewing, coding, reviewing, testing, deep-reviewing, accepting) |
| `runner` | Sobrescritura opcional del runner para esta phase |
| `model` | Sobrescritura opcional del tier de modelo para esta phase |

**Precedencia de selección:**

1. `4x run --profile <name>` — sobrescritura explícita (se busca en `profiles`, luego en los valores predeterminados integrados).
2. De lo contrario, si existe una sección `profiles`, selección automática por la `priority` del feature: `null`/`0`/`1` -> `full`, `2` -> `normal`, `>=3` -> `quick`.
3. Si no existe sección `profiles`, todos los features ejecutan `full` (la selección automática por prioridad está deshabilitada — retrocompatible).

Los tres perfiles integrados (`full`/`normal`/`quick`) siempre están disponibles como respaldo incluso sin una sección `profiles`. El nombre del perfil activo se registra en el estado del feature y se muestra en la tarjeta del dashboard.

Cuando `parallel_review_test` es `true` y el perfil activo habilita tanto `reviewer` como `tester`, los dos roles de solo lectura se ejecutan concurrentemente en el mismo worktree durante la fase de reviewing; si ambos aprueban, se avanza al deep review; de lo contrario, el ciclo re-entra en coding.

## Configuración del usuario (`~/.4x/settings.json`)

Preferencias globales del usuario y valores predeterminados de runners. Configuración entre proyectos gestionada vía `4x config` o el editor de **Configuración global** del dashboard (botón ⚙G en la barra lateral).

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### Campos de configuración del usuario

| Campo | Descripción |
|---|---|
| `locale` | Idioma para las instrucciones de los prompts de roles |
| `theme` | Tema del dashboard (`dark`/`light`) |
| `default_runner` | Nombre del runner predeterminado (sobrescrito por el proyecto) |
| `runners` | Definiciones de runners (command, args, tty, etc.) |
| `roles` | Modelos predeterminados por rol |
| `logLevel` | Nivel mínimo de log (debug/info/warn/error; predeterminado "info"; sobrescrito por la variable de entorno FOURX_LOG_LEVEL) |
| `logRetainDays` | Días de retención de archivos de log en ~/.4x/logs/ (predeterminado 7) |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args` es un campo de tipo array — edita `~/.4x/settings.json` directamente para configurarlo.

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

## Fusión de configuraciones

Cuando `4x run` o `4x prompt` se ejecuta, las configuraciones a nivel de usuario y proyecto se fusionan en profundidad:

- **Prioridad:** proyecto > usuario > valores predeterminados
- **Fusión de runners:** por campo — los campos no vacíos del proyecto sobrescriben los del usuario. `args` se reemplaza completamente (no se concatena). `tiers` se fusiona a nivel de clave.
- **Fusión de roles:** por campo — igual que runners.
- **Campos exclusivos del proyecto**: todos los campos excepto `default_runner`, `runners` y `roles` son exclusivos del proyecto y nunca se sobrescriben por la configuración del usuario.

El editor de configuración del proyecto en el dashboard muestra la configuración **cruda** del proyecto, no el resultado fusionado. Usa la pestaña **Merged** en la configuración del proyecto para ver la configuración efectiva final después de la fusión.
