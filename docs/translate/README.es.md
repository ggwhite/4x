<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/banner-dark.svg">
  <img src="../assets/banner-light.svg" alt="4X: Diseñar. Programar. Revisar. Probar." width="100%">
</picture>

**Un bucle de desarrollo agéntico que divide Design, Code, Review y Test en roles de IA aislados, controlados por guardrails deterministas.**

<p>
  <a href="https://github.com/ggwhite/4x/actions/workflows/ci.yml"><img src="https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ggwhite/4x/releases"><img src="https://img.shields.io/github/v/release/ggwhite/4x" alt="Última versión"></a>
  <a href="https://pkg.go.dev/github.com/ggwhite/4x"><img src="https://pkg.go.dev/badge/github.com/ggwhite/4x.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/ggwhite/4x"><img src="https://goreportcard.com/badge/github.com/ggwhite/4x" alt="Go Report Card"></a>
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="Licencia: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey" alt="Plataforma: macOS, Linux, Windows">
</p>

[Instalación](#instalación) · [Inicio rápido](#inicio-rápido) · [Funciones](#funciones) · [Cómo funciona el bucle](#cómo-funciona-el-bucle) · [Guardrails](#guardrails-deterministas) · [Arquitectura](#arquitectura) · [Documentación](#documentación) · [FAQ](#preguntas-frecuentes)

[English](../../README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | **Español**

</div>

---

<div align="center">
  <a href="../assets/tour.mp4"><img src="../assets/tour.webp" alt="Recorrido por 4x, acelerado a partir de una ejecución real: (1) 4x init configura un proyecto Go existente llamado greet y detecta los comandos go build y go test; (2) 4x new crea la feature F001 Greet by name; (3) 4x run F001 hace pasar a Claude Code por Designer, Coder, Reviewer y Tester; la allowlist de verify bloquea las comprobaciones del Tester, así que la feature escala de vuelta al Designer, que las reescribe, y la segunda ronda llega a Accept y pending-review mostrando el coste total; (4) 4x status lista la feature en pending-review, 4x done F001 la marca como terminada y go run . -name Ada imprime hello, Ada" width="900"></a>
  <br>
  <sub>▶ <a href="../assets/tour.mp4">Ver el recorrido completo</a> · una ejecución real de Claude Code (unos 4 minutos, $2), con las esperas del agente recortadas</sub>
</div>

## Por qué

Programar con un solo agente es rápido pero frágil. Le pides a una única IA que diseñe, implemente, revise y pruebe, todo de un tirón y con los mismos sesgos. Funciona para tareas pequeñas. Se desmorona con features reales.

4x divide el bucle. Cada rol tiene un trabajo concreto, un alcance limitado y ningún acceso al razonamiento de los demás: el Designer no escribe código, el Coder no juzga su propio trabajo, el Reviewer es adversarial por diseño y el Tester valida contra criterios escritos antes de la implementación. Es la CLI en Go, no un LLM, la que decide qué puede pasar a continuación. Como en los juegos de estrategia 4X (eXplore, eXpand, eXploit, eXterminate), roles distintos con fortalezas distintas convergen en un mismo objetivo: features que sobreviven al contacto con producción.

## Funciones

<table>
<tr>
<td width="42%" valign="middle">

### Cuatro roles aislados

**Designer → Coder → Reviewer → Tester → Accept**, cada uno es un proceso runner independiente que solo se comunica mediante archivos en `.4x/`. Un **FAIL** del Reviewer o una verificación fallida envía el código a **amending**; una especificación errónea **escala** de vuelta al Designer. Los **profiles** del pipeline (`full`, `normal`, `lite`, `quick` o uno propio) eligen qué fases se ejecutan.

</td>
<td><img src="../assets/feature-loop.webp" alt="Pestaña Messages de 4x Live durante la ejecución de demostración: aparecen los mensajes del Designer sobre task-brief.md y acceptance-criteria.md y luego los informes del Coder, el Reviewer y el Tester uno tras otro, cada uno con su modelo y coste; el Tester escala, el Designer reescribe el brief y, tras una segunda pasada de Coder, Reviewer y Tester, el Acceptor cierra la feature en Review"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### Guardrails deterministas

Los guardrails son **código Go, no prompts**. `4x check` verifica el **scope**, la instantánea **baseline**, los archivos obligatorios y la **evidencia de verify**; la **máquina de estados** rechaza saltos de fase ilegales; `4x verify` solo ejecuta comandos de la **allowlist** del proyecto. Ningún runner puede saltárselos a base de palabras.

</td>
<td><img src="../assets/feature-guardrails.webp" alt="Terminal: 4x check F001 informa de que la feature de demostración terminada supera todas las comprobaciones de guardrails; 4x transition F001 --to coding --json se rechaza con invalid transition: pending-review to coding; 4x verify F001 ejecuta las comprobaciones go build, go vet y go test de test-strategy.yaml por cada criterio de aceptación e imprime PASSED"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### Dashboard 4x Live

`4x live` sirve un **dashboard multiproyecto** sobre REST + **SSE**: las tarjetas de features pasan de Todo a Running y a Review según cambian las fases, con rondas, runner y coste por feature. La misma interfaz web va integrada en el binario y envuelta en las apps de **macOS (Swift)** y **Windows / Linux (Tauri)**.

</td>
<td><img src="../assets/feature-live.webp" alt="Vista general del proyecto en 4x Live durante la ejecución de demostración: la tarjeta F001 Greet by name pasa de Todo a Running, su etiqueta de fase avanza por designing, coding, reviewing y testing mientras el contador Running, el gráfico de anillo de estados y el panel Currently running la siguen; al final la tarjeta queda en Review con un botón Mark Done y el panel Pending review la muestra"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### Coste por rol y por ronda

Cada llamada a un runner queda registrada con su modelo, duración y **coste en USD o tokens**. `4x cost` lo agrega **por rol**, `--by-round` muestra cuánto se fue en **reintentos** y `--feature` desglosa una feature ronda a ronda, para que veas lo que costó realmente un fallo de revisión.

</td>
<td><img src="../assets/feature-cost.webp" alt="Terminal: 4x cost muestra el gasto del proyecto de demostración por rol (designer, coder, reviewer, tester, acceptor) con llamadas, total, media y porcentaje; después 4x cost --feature F001-greet-by-name lista las mismas llamadas por ronda y rol"></td>
</tr>
</table>

### Más

- **7 runners**: Claude Code, Codex, Gemini CLI, Antigravity, Copilot, Cursor y OpenCode comparten el mismo protocolo de archivos `.4x/`. Combínalos por rol o por fase (`--phase-override`).
- **Pipeline adaptativo**: un asesor de profiles sugiere `full` / `normal` / `quick` según la complejidad de la feature; los modelos por rol usan los niveles `strong` / `fast`.
- **Recuperación ante fallos**: si un runner se cae, se reanuda desde el último estado guardado; los errores transitorios de la API (red, límites de uso) se reintentan con backoff.
- **Aprobación humana**: toda feature se detiene en `pending-review`; `4x done` la marca como terminada y fusiona su rama (o, con `issue_tracker`, abre el PR/MR).
- **Flujo de MR con issue primero**: modo `issue_tracker` opcional. `4x new` crea o vincula un issue y `4x done` sube la rama y abre un PR/MR en lugar de fusionar en local. GitHub o GitLab (incluido el autoalojado) se detecta a partir del remote de cada repo.
- **Dependencias**: `4x new --depends F001` bloquea una feature hasta que sus dependencias estén terminadas; el dashboard dibuja el grafo de dependencias.
- **Autoevolución**: `4x mine` / `4x gate` / `4x evolve` extraen señales de fallo de ejecuciones pasadas, filtran candidatos con una compuerta de valor y encolan features de mejora. Las lecciones de las retrospectivas (`4x learn`) se inyectan en los prompts de roles posteriores.
- **Servidor MCP** y **skills de Claude Code** (`4x-audit`, `4x-autopilot`), ver más abajo.
- **Más de 30 comandos de CLI**: `run`, `status`, `check`, `verify`, `retry`, `cost`, `live`, `doctor`, `clean`, `mcp`, `skills`, hooks de fase, logging estructurado y más. Consulta la [referencia de la CLI](../guide/es/cli.md).

## Instalación

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Script de shell

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### Descargar el binario

Hay binarios precompilados para macOS, Linux y Windows (amd64 / arm64) en la página de [Releases](https://github.com/ggwhite/4x/releases).

<details>
<summary><b>Verificar checksums</b> (descargas manuales)</summary>

El script de shell verifica automáticamente el checksum de la descarga y se detiene si falla. Si descargas a mano un archivo o binario desde la página de [Releases](https://github.com/ggwhite/4x/releases), verifícalo tú antes de extraerlo: descarga el `checksums.txt` de esa versión en el mismo directorio y ejecuta uno de estos comandos:

```bash
# Linux (and macOS with coreutils)
sha256sum --check --ignore-missing checksums.txt

# macOS (default)
shasum -a 256 --ignore-missing --check checksums.txt
```

Extrae y ejecuta el binario solo cuando el checksum indique `OK`.

</details>

## Inicio rápido

```bash
# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication (User authentication with OAuth2)

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` impulsa el bucle Design-Code-Review-Test de forma automática. Si Review encuentra problemas, Code hace otra pasada. Si Test falla, el bucle itera. Mantienes el control con `--max-rounds`, `--profile` y `--timeout`.

## Los cuatro roles

| Rol | Trabajo | Salidas |
|---|---|---|
| **Designer** | Analiza los requisitos y produce la especificación y los criterios de aceptación | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` |
| **Coder** | Implementa exactamente lo que dice la especificación | Código fuente, `coder-report.md` |
| **Reviewer** | Detecta bugs e incumplimientos de la especificación (checklist + enfoque adversarial) | `review-report.md` con veredicto |
| **Tester** | Valida contra los criterios de aceptación con evidencia | `test-report.md`, `verify.json` |

Cada rol está **aislado**. El Coder nunca ve el feedback previo del Reviewer. El Tester valida contra criterios escritos por el Designer, no por el Coder. Esta separación evita los puntos ciegos que plagan los flujos de un solo agente. El profile `full` añade un Design Reviewer, un Deep Reviewer con múltiples ángulos, un Fixer y un Acceptor alrededor de los cuatro roles principales.

## Cómo funciona el bucle

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/loop-dark.svg">
  <img src="../assets/loop-light.svg" alt="Designer, Coder, Reviewer, Tester, Accept, Pending Review y Done en fila. Un FAIL del Reviewer o una verificación fallida envía el código a amending, que vuelve al Reviewer; las escalaciones regresan al Designer; una persona aprueba con 4x done antes de Done." width="100%">
</picture>

- **Fallo de revisión** (veredicto FAIL o hallazgos CRITICAL) devuelve el código a amending
- **Fallo de pruebas** (verify no superado) devuelve el código a amending
- **Escalación** (especificación incoherente, criterios incorrectos) vuelve al Designer
- **La compuerta pending review** garantiza que una persona revise siempre antes de marcar como terminado
- **El presupuesto de rondas** (5 por defecto) evita bucles infinitos

Las transiciones legales están en [`internal/state/machine.go`](../../internal/state/machine.go); consulta [Conceptos básicos](../guide/es/concepts.md) para ver la máquina de estados completa.

## Guardrails deterministas

Los aplica la CLI, no el juicio de una IA:

| Guardrail | Qué hace |
|---|---|
| **Comprobación de scope** | Los archivos modificados deben estar dentro de los repos declarados |
| **Instantánea baseline** | Se captura el estado previo a la codificación para poder revertir con seguridad |
| **Máquina de estados** | Las fases deben avanzar en un orden legal |
| **Requisito de evidencia** | El Tester debe aportar `verify.json` con la salida de los comandos |
| **Compuerta de pruebas** | Se exigen `verify.json` + informe de pruebas + informe final |
| **Allowlist de verify** | `4x verify` solo ejecuta los comandos listados en `verify_command_allowlist` |
| **Compuerta de dependencias** | Las features con dependencias pendientes no pueden empezar |

## Arquitectura

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/architecture-dark.svg">
  <img src="../assets/architecture-light.svg" alt="Tú manejas la CLI de 4x (Go): guardrails deterministas, sin llamadas a LLM, con comprobaciones de scope, protocolo, máquina de estados y control de dependencias. Pasa el trabajo a los runners (Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor, OpenCode) mediante el protocolo de archivos del directorio .4x/. 4x Live recibe eventos SSE y muestra la monitorización multiproyecto en tiempo real." width="100%">
</picture>

**Capa 1: CLI** se encarga de todo lo determinista: validación de scope, transiciones de estado, instantáneas baseline y recogida de evidencia. Nunca llama a un LLM, así que los guardrails no dependen del juicio de una IA.

**Capa 2: Runners** conectan el protocolo de la CLI con la herramienta de IA que elijas. Todos hablan el mismo protocolo de archivos `.4x/`, pero usan las capacidades nativas de su plataforma.

**Capa 3: Live** es el dashboard multiproyecto. Observa a tus agentes trabajar en tiempo real, sigue las transiciones de fase y transmite los logs. API REST + SSE.

## Autopilot

Encola varias features (usa `--depends` para ordenarlas) y deja que el skill de Claude Code `4x-autopilot` las conduzca: elige la siguiente feature disponible, la ejecuta, la aprueba, la fusiona y pasa a la siguiente, sin supervisión. Sustituye al antiguo comando `4x batch`.

## Servidor MCP

`4x mcp` arranca un servidor Model Context Protocol sobre stdio. Expone la CLI a los clientes MCP como herramientas como `4x_status`, `4x_new`, `4x_run`, `4x_stop`, `4x_check`, `4x_verify`, `4x_done`, `4x_doctor` y `4x_learn_*`.

```bash
4x mcp
```

## Skills de Claude Code

4x incluye dos [Claude Code Skills](https://docs.anthropic.com/en/docs/claude-code) opcionales para dirigir el pipeline desde una sesión de Claude Code. Instálalos con [`npx skills`](https://github.com/vercel-labs/skills), sin necesidad de clonar este repo:

```bash
npx skills add ggwhite/4x --skill 4x-audit       # scan past run artifacts, report + optionally file gap features
npx skills add ggwhite/4x --skill 4x-autopilot   # drive the full pick → run → merge → next loop unattended
npx skills add ggwhite/4x --all                  # install both
```

- **`4x-audit`**: analiza discovered-feature-gaps, los informes de escalación y revisión y los patrones recurrentes de learnings; genera un informe HTML por categorías y, opcionalmente, puede crear YAMLs de features en lote.
- **`4x-autopilot`**: consulta el estado de 4x y lleva las features por todo su ciclo de vida (design → code → review → test → merge) sin pararse a pedir confirmación, incluida la fusión. **Solo para el propietario**: lee la advertencia del skill antes de usarlo en un repo compartido.

Desde un clon de este repo, `4x skills install <name>` enlaza un skill en `~/.claude/skills/` mediante un symlink.

## Modelo de permisos

> [!WARNING]
> **4x ejecuta agentes de IA en modo no interactivo.** Usa 4x solo en proyectos donde te parezca bien que los agentes de IA actúen de forma autónoma.

<details>
<summary>Qué significa esto</summary>

Durante `4x init`, los runners se configuran con flags que omiten las solicitudes de permisos (`--dangerously-skip-permissions`, `-y`, `--yolo`) para que el bucle funcione de forma autónoma.

Los guardrails deterministas de la CLI (bloqueo de scope, instantáneas baseline, máquina de estados, allowlist de verify) marcan el límite de seguridad. Antes de lanzar un runner, 4x también filtra de su entorno las variables sensibles (`*_TOKEN`, `*_KEY`, `*_SECRET`, `AWS_*`, ...); consulta [Runners y plugins](../guide/es/runners.md).

</details>

## Compromisos

Elegir 4x significa cambiar velocidad y coste por estructura y corrección. Sé honesto sobre si tu proyecto necesita ese intercambio.

<details>
<summary><b>Fortalezas, debilidades y cuándo (no) usar 4x</b></summary>

### Fortalezas

- **El aislamiento de roles elimina el sesgo de autorrevisión.** El Coder nunca juzga su propio trabajo. El Reviewer es adversarial por diseño. Los flujos de un solo agente dejan que el mismo modelo escriba y apruebe el código; 4x no.
- **Los guardrails deterministas no dependen del juicio de una IA.** Bloqueo de scope, máquina de estados, requisitos de evidencia: los aplica la CLI en Go, no un prompt que le pide al LLM «por favor, no te salgas del scope».
- **El protocolo basado en archivos lo hace independiente del LLM.** Cambia entre Claude, Gemini y Codex, o combínalos por rol. Sin dependencia de proveedor ni de SDK.
- **Estado resistente a fallos.** Todo vive en archivos de `.4x/`. Si la sesión muere o la máquina se reinicia, `4x run` retoma exactamente donde se quedó.
- **La persona sigue en el bucle.** La compuerta `pending-review` garantiza que una persona revise siempre el trabajo de la IA antes de marcarlo como terminado. La IA propone, tú decides.
- **Domestica las refactorizaciones a gran escala.** Los cambios demasiado grandes para una sola sesión de IA (dividir god objects, extraer paquetes, migrar APIs) pueden descomponerse en features dependientes con los profiles adecuados. 4x gestiona la secuencia, la revisión y la verificación a lo largo de varias fases que desbordarían una sola ventana de contexto.
- **Escala a un backlog.** Las dependencias junto con el skill autopilot te permiten encolar decenas de features por la noche y revisarlas por la mañana.

### Debilidades

- **Coste en tokens bastante mayor.** Cada feature pasa como mínimo por 4 o más llamadas separadas a un LLM. Un fallo de revisión lo duplica. Cuenta con entre 3 y 10 veces el coste en tokens de un enfoque de un solo agente para la misma tarea. Consulta los [Consejos de uso](../guide/es/usage-tips.md) para estimaciones de coste.
- **Más lento en tareas simples.** Corregir un bug de una línea no necesita un Designer, un Reviewer y un Tester. La sobrecarga del bucle completo se desperdicia en cambios triviales. Usa herramientas de un solo agente para arreglos rápidos.
- **Coste de configuración.** `4x init`, el YAML de la feature, la configuración: hay cierto ceremonial antes de empezar. No compensa para un script de usar y tirar.
- **Estructura de bucle rígida.** La secuencia Design → Code → Review → Test es fija. Si tu flujo de trabajo no encaja en cuatro roles, acabarás peleándote con el framework en lugar de usarlo.
- **La calidad depende de la calidad del prompt.** Las descripciones de features vagas producen especificaciones vagas, que producen código incorrecto. 4x añade estructura, pero si entra basura sigue saliendo basura, solo que con más pasos.

### Cuándo usar 4x

- Features que tienen que ser correctas (pagos, autenticación, pipelines de datos)
- Trabajo que se beneficia de una revisión adversarial (código sensible en seguridad)
- Ir despachando un backlog de features
- Equipos que quieren trazabilidad auditable del código generado por IA

### Cuándo NO usar 4x

- Arreglos rápidos puntuales o prototipos exploratorios
- Tareas en las que la velocidad importa más que la corrección
- Proyectos con un presupuesto de tokens ajustado
- Sesiones de hacking en solitario en las que de todos modos revisarías el código tú mismo

</details>

## Documentación

| Documento | Descripción |
|---|---|
| **[Guía de usuario](../guide/es/)** | Documentación de uso completa |
| [Primeros pasos](../guide/es/getting-started.md) | Instalación y primera ejecución |
| [Referencia de la CLI](../guide/es/cli.md) | Todos los comandos y flags |
| [Conceptos básicos](../guide/es/concepts.md) | Roles, máquina de estados, protocolo, guardrails |
| [Configuración](../guide/es/configuration.md) | Ajustes, modelos, idioma, runners |
| [Runners y plugins](../guide/es/runners.md) | Runners compatibles y contrato de plugins |
| [Dashboard](../guide/es/dashboard.md) | Dashboard multiproyecto 4x Live |
| [Consejos de uso](../guide/es/usage-tips.md) | Elección de modelo, coste, solución de problemas |

<details>
<summary><b>Estructura del proyecto</b></summary>

```
4x/
  cmd/4x/              CLI entry point (Cobra), one file per subcommand
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    orchestrator/       Run loop orchestration
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
    mcp/                MCP server (stdio)
  plugins/
    claude-code/        Claude Code runner instructions
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions
    cursor/             Cursor rules
    opencode/           OpenCode runner instructions
    embed.go            go:embed plugin files into binary
  dashboard/
    web/                Dashboard UI, embedded into the binary
    macos/              Swift native app
    tauri/              Windows / Linux app
  skills/               4x-audit, 4x-autopilot Claude Code skills
  templates/            Role prompt templates
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

</details>

## Preguntas frecuentes

<details>
<summary><b>¿4x llama directamente a alguna API de LLM?</b></summary>

No. La CLI es Go puro, sin ninguna dependencia de LLM. Los runners gestionan toda la interacción con la IA usando las capacidades nativas de su plataforma.

</details>

<details>
<summary><b>¿Puedo usar LLMs distintos para roles distintos?</b></summary>

Sí. Configura los modelos por rol en `.4x/settings.json`. Usa Claude para Design y Gemini para Code: cada uno lee los mismos archivos de `.4x/`.

</details>

<details>
<summary><b>¿En qué se diferencia de Devin / SWE-agent / OpenHands?</b></summary>

Esos son agentes autónomos que lo hacen todo de una vez. 4x es un *framework* que estructura la colaboración entre varios roles con guardrails deterministas. Se parece más a un pipeline de CI para IA que a un único agente autónomo.

</details>

## Desarrollo

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
make build      # bin/4x
make test       # go test -race ./...
make lint       # go vet + gofmt + golangci-lint
```

Consulta [CONTRIBUTING.md](../../CONTRIBUTING.md) para el flujo de trabajo completo.

<details>
<summary><b>Volver a grabar los clips y diagramas del README</b></summary>

Todos los clips salen del binario real, compilado desde el código fuente, en un proyecto desechable y un `HOME` bajo `/tmp/4xdemo`. La grabación es headless: [`vhs`](https://github.com/charmbracelet/vhs) para la terminal y Python Playwright (Chromium headless) para el dashboard. Hacen falta `vhs`, `ffmpeg`, `img2webp` (libwebp), `playwright` para Python y, para el recorrido, una CLI `claude` con sesión iniciada.

```bash
docs/assets/record/render.sh tour      # fresh /tmp/4xdemo, ONE real 4x run (about $2 of tokens), all clips
docs/assets/record/render.sh recut     # re-encode the clips from the last run's frames, no agent run
python3 docs/assets/diagrams/gen.py    # architecture / loop SVGs (light + dark)
python3 docs/assets/diagrams/snapshot.py /tmp/svg docs/assets/*-light.svg   # check frames + reduced motion
```

`render.sh tour` ejecuta [`setup-demo.sh`](../assets/record/setup-demo.sh) (compila 4x desde el código fuente y crea un pequeño proyecto Go, "greet"), graba [`tour.tape`](../assets/record/tour.tape) mientras [`capture-live.py`](../assets/record/capture-live.py) hace capturas de `4x live`, graba los clips de funciones y, por último, [`tour-end.tape`](../assets/record/tour-end.tape) aprueba la feature. [`retime.py`](../assets/record/retime.py) recorta las largas esperas del agente y codifica el WebP con `img2webp`. El runner de Claude Code se invoca a través de un wrapper que omite los ajustes del usuario, los hooks, los plugins y los servidores MCP.

Antes de hacer commit de clips nuevos, pasa OCR por cada fotograma (`swiftc docs/assets/record/ocr.swift -o /tmp/ocr`, luego `magick clip.webp -coalesce f-%03d.png` y `/tmp/ocr f-*.png`) y busca con grep nombres de usuario, nombres de host, correos y tokens en el texto.

</details>

## Origen

4x nació dentro de un sistema de producción llamado DCT (Designer-Coder-Tester), que entregó más de 60 features en la reescritura de una plataforma a gran escala. Los patrones que sobrevivieron (aislamiento de roles, protocolo basado en archivos, comprobación determinista del scope, pruebas basadas en evidencia) se convirtieron en 4x. Lo que no sobrevivió (trucos específicos de cada LLM, suposiciones de contexto compartido, guardrails basados en la confianza) se dejó fuera a propósito.

## Licencia

[MIT](../../LICENSE)

---

<p align="center">
  <strong>Deja de esperar que tu IA escriba código correcto. Empieza a verificarlo.</strong>
</p>
