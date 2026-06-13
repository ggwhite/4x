# Primeros pasos

## Instalación

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/4x
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

Si `.4x/` ya existe, `init` termina con un error — usa `4x upgrade` para actualizar los archivos de plugins.

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

## Actualizar archivos de plugins

Cuando actualices el binario `4x`, vuelve a desplegar los plugins embebidos:

```bash
4x upgrade            # deploy new files
4x upgrade --dry-run  # preview changes only
```

## Próximos pasos

- [Referencia del CLI](cli.md) — todos los comandos y banderas
- [Conceptos principales](concepts.md) — entender roles, máquina de estados, protocolo
- [Configuración](configuration.md) — personalizar modelos, runners, locale
