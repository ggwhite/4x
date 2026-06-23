# Primeros pasos

## Instalación

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

Requiere Go 1.26+.

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### Descargar binario

Los binarios precompilados para macOS, Linux y Windows (amd64 / arm64) están disponibles en la página de [Releases](https://github.com/ggwhite/4x/releases).

### macOS Gatekeeper

El binario CLI y la app 4x Live dashboard no están firmados con un certificado de Apple Developer. macOS Gatekeeper los bloqueará en el primer lanzamiento. Dos formas de solucionarlo:

**Opción A: Eliminar atributo de cuarentena (recomendado)**

```bash
# Binario CLI
xattr -cr /usr/local/bin/4x

# App del dashboard
xattr -cr /Applications/4x\ Live.app
```

**Opción B: Permitir desde Configuración del Sistema**

1. Haga doble clic en la app — macOS muestra "no se puede abrir porque no se puede verificar al desarrollador"
2. Abra **Configuración del Sistema → Privacidad y Seguridad**
3. Desplácese hasta la sección **Seguridad** — verá un mensaje sobre la app bloqueada
4. Haga clic en **Abrir de todos modos**
5. Ingrese su contraseña o use Touch ID para confirmar
6. La app se abrirá; macOS recordará su elección para futuros lanzamientos

### Windows SmartScreen

El binario no está firmado con un certificado de firma de código. Chrome y Edge pueden bloquear la descarga, y Windows SmartScreen puede bloquear la ejecución.

**Descarga bloqueada por el navegador:**

1. Chrome: haga clic en la advertencia de descarga → **Conservar** → **Conservar de todos modos**
2. Edge: haga clic en `...` en la barra de descarga → **Conservar** → **Mostrar más** → **Conservar de todos modos**

**Ejecución bloqueada por SmartScreen:**

1. Haga doble clic en el exe — Windows muestra "Windows protegió su PC"
2. Haga clic en **Más información**
3. Haga clic en **Ejecutar de todos modos**

O desbloquee mediante PowerShell:

```powershell
Unblock-File -Path .\4x.exe
```

### Verificar

Verifica con:

```bash
4x --help
```

## Inicializar un proyecto

```bash
cd my-project
4x init
```

Esto crea un directorio `.4x/` con:
- `settings.json` — configuración del proyecto, definiciones de runners, mapeo de modelos por rol
- `plugins/` — archivos de instrucciones para runners (SKILL.md, AGENTS.md, etc.)
- Archivos de importación en el nivel raíz (CLAUDE.md, AGENTS.md, GEMINI.md, etc.)

4x detecta automáticamente el lenguaje de tu proyecto (Go, TypeScript, Java, Rust, Python) y pre-llena los comandos de build/test/lint.

Si `.4x/` ya existe, `init` termina con un error — usa `4x sync` para actualizar los archivos de plugins.

## Crear un feature

```bash
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Los features se almacenan en `.4x/features/{id}.yaml`. El formato del ID es `F{NNN}-{slug}` (slug de hasta 23 caracteres).

Usa `--repo` para declarar qué repositorios están dentro del alcance (para proyectos multi-repo).

## Ejecutar el ciclo

```bash
# Run with default runner (usually claude)
4x run F001

# Specify a runner
4x run F001 --runner claude

# Limit iterations
4x run F001 --max-rounds 3

# Set timeout (seconds)
4x run F001 --timeout 7200

# Preview prompts without calling LLM
4x run F001 --dry-run
```

Los IDs de features soportan coincidencia por prefijo insensible a mayúsculas — tanto `4x run F001` como `4x run f001` funcionan.

El ciclo ejecuta: **Design → Code → Review → Test → Accept → Pending Review**. Si Review encuentra problemas, Code recibe otra pasada. Si Test falla, el ciclo itera (hasta `--max-rounds`).

## Verificar estado

```bash
# All features
4x status

# Single feature with details
4x status F001

# Filter pending review
4x status --pending
```

## Completar un feature

Después de que el ciclo termina, el feature queda en estado `pending-review` — esperando la aprobación humana.

```bash
# Review the outputs
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# Mark as done
4x done F001
```

## Control de versiones

`4x init` crea un `.4x/.gitignore` que excluye los artefactos de ejecución. Haz commit del resto:

| Ruta | Rastrear | Razón |
|---|---|---|
| `.4x/settings.json` | **Sí** | Configuración del proyecto — compartida en el equipo |
| `.4x/features/*.yaml` | **Sí** | Definiciones de features |
| `.4x/learnings.json` | **Sí** | Base de conocimiento retrospectivo entre features |
| `.4x/candidates.json` | **Sí** | Pool de features candidatos auto-descubiertos |
| `.4x/plugins/` | **Sí** | Archivos de instrucciones del runner |
| `.4x/run/` | **No** | Artefactos de ejecución (estado, logs, reportes) — excluidos automáticamente por `.gitignore` |

Para proyectos existentes inicializados antes de esta característica, agrega el gitignore manualmente:

```bash
printf 'run/\ngate-input.json\ngate-verdicts.json\nevolve-state.json\n' > .4x/.gitignore
```

## Actualizar archivos de plugins

Cuando actualices el binario `4x`, vuelve a desplegar los plugins embebidos:

```bash
4x sync            # deploy new files
4x sync --dry-run  # preview changes only
```

## Próximos pasos

- [Referencia del CLI](cli.md) — todos los comandos y banderas
- [Conceptos principales](concepts.md) — entender roles, máquina de estados, protocolo
- [Configuración](configuration.md) — personalizar modelos, runners, locale
