[English](README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | **Español**

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

<p align="center">
  <img src="docs/assets/4x-banner.svg" alt="4X — Design. Code. Review. Test." width="480">
</p>

<p align="center">
  <img src="docs/assets/demo.gif" alt="4x demo" width="720">
</p>

**4x es un framework de desarrollo con IA multi-rol que divide el ciclo de ingeniería de software en cuatro fases especializadas** — Design, Code, Review, Test — cada una impulsada por un agente de IA dedicado. Al igual que los juegos de estrategia 4X (eXplore, eXpand, eXploit, eXterminate), el nombre refleja un sistema donde roles distintos con fortalezas distintas convergen para conquistar la complejidad.

---

## ¿Por qué 4x?

La programación con un solo agente es rápida pero frágil. Le pides a una sola IA que diseñe, implemente, revise y pruebe — todo al mismo tiempo, con los mismos sesgos. Funciona para tareas pequeñas. Se desmorona en features reales.

4x divide el ciclo. Cada rol tiene un trabajo enfocado, alcance limitado y sin acceso al razonamiento de los demás. El Designer no escribe código. El Coder no juzga su propio trabajo. El Reviewer es adversarial por diseño. El Tester valida contra criterios escritos antes de la implementación.

El resultado: features que sobreviven al contacto con producción.

## Ventajas y desventajas

Elegir 4x significa intercambiar velocidad y costo por estructura y corrección. Sé honesto sobre si tu proyecto necesita ese intercambio.

### Fortalezas

- **El aislamiento de roles elimina el sesgo de auto-revisión.** El Coder nunca juzga su propio trabajo. El Reviewer es adversarial por diseño. Los flujos de un solo agente permiten que el mismo modelo escriba y apruebe código — 4x no.
- **Los guardrails determinísticos no dependen del juicio de la IA.** Bloqueo de alcance, máquina de estados, requisitos de evidencia — son aplicados por el CLI en Go, no pidiendo a un LLM que "por favor se mantenga en el alcance."
- **El protocolo basado en archivos lo hace agnóstico al LLM.** Cambia entre Claude, Gemini, Codex, o mézclalos por rol. Sin dependencia de proveedor, sin dependencia de SDK.
- **Estado resistente a fallos.** Todo vive en archivos `.4x/`. La sesión muere, la máquina se reinicia — `4x run` continúa exactamente donde se detuvo.
- **El humano permanece en el ciclo.** La compuerta `pending-review` asegura que un humano siempre revise el trabajo de la IA antes de marcarlo como terminado. La IA propone, tú dispones.
- **El modo batch escala.** La programación con reconocimiento de dependencias te permite encolar docenas de features durante la noche y revisarlos por la mañana.

### Debilidades

- **Costo de tokens significativamente mayor.** Cada feature pasa por al menos 4+ llamadas separadas al LLM como mínimo. Un fallo en la revisión duplica eso. Espera 3-10x el costo de tokens de un enfoque de un solo agente para la misma tarea. Consulta [Consejos de uso](docs/guide/es/usage-tips.md) para estimaciones de costos.
- **Más lento para tareas simples.** Un bug fix de una línea no necesita un Designer, Reviewer y Tester. La sobrecarga del ciclo completo se desperdicia en cambios triviales. Usa herramientas de un solo agente para correcciones rápidas.
- **Costo de configuración.** `4x init`, YAML del feature, configuración de settings — hay ceremonia antes de empezar. No vale la pena para un script desechable.
- **Estructura de ciclo rígida.** La secuencia Design → Code → Review → Test es fija. Si tu flujo de trabajo no encaja en cuatro roles, lucharás contra el framework en vez de usarlo.
- **La calidad depende de la calidad del prompt.** Descripciones vagas de features producen specs vagas, que producen código incorrecto. 4x agrega estructura, pero basura entra, basura sale — solo que con más pasos.

### Cuándo usar 4x

- Features que necesitan ser correctos (pagos, autenticación, pipelines de datos)
- Trabajo que se beneficia de revisión adversarial (código sensible a seguridad)
- Procesamiento batch de un backlog de features
- Equipos que quieren registros de auditoría del código generado por IA

### Cuándo NO usar 4x

- Correcciones rápidas puntuales o prototipado exploratorio
- Tareas donde la velocidad importa más que la corrección
- Proyectos donde el presupuesto de tokens es ajustado
- Sesiones de hacking en solitario donde revisarías el código tú mismo de todos modos

## Arquitectura

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**Capa 1 — CLI** maneja todo lo determinístico: validación de alcance, transiciones de estado, snapshots de baseline, recolección de evidencia. Nunca llama a un LLM. Los guardrails no dependen del juicio de la IA.

**Capa 2 — Runners** conectan el protocolo del CLI con tu herramienta de IA preferida. Claude Code, Codex, Gemini, Antigravity, Copilot, Cursor — cada uno habla el mismo protocolo de archivos `.4x/` pero usa las capacidades nativas de la plataforma.

**Capa 3 — Live** es el dashboard multi-proyecto. Observa a tus agentes de IA trabajar en tiempo real, ve las transiciones de fase, transmite logs. API REST + SSE.

## Instalación

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/4x
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### Descargar binario

Los binarios precompilados para macOS, Linux y Windows (amd64 / arm64) están disponibles en la página de [Releases](https://github.com/ggwhite/4x/releases).

## Inicio rápido

```bash
# Inicializar en tu proyecto
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` impulsa el ciclo Design-Code-Review-Test automáticamente. Si Review encuentra problemas, Code recibe otra pasada. Si Test falla, el ciclo itera. Tú mantienes el control con las banderas `--max-rounds` y `--timeout`.

## Los cuatro roles

| Rol | Trabajo | Productos |
|---|---|---|
| **Designer** | Analizar requisitos, producir spec + criterios de aceptación | `task-brief.md`, `acceptance-criteria.md` |
| **Coder** | Implementar exactamente lo que dice la spec | Código fuente, `coder-report.md` |
| **Reviewer** | Detectar bugs y violaciones de la spec (checklist + adversarial) | `review-report.md` con veredicto |
| **Tester** | Validar contra criterios de aceptación con evidencia | `test-report.md`, `verify.json` |

Cada rol está **aislado**. El Coder nunca ve la retroalimentación previa del Reviewer. El Tester valida contra criterios escritos por el Designer, no por el Coder. Esta separación previene los puntos ciegos que afectan a los flujos de trabajo de un solo agente.

## Cómo funciona el ciclo

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **Fallo en Review** (veredicto FAIL o hallazgos CRITICAL) envía el código de vuelta para enmienda
- **Fallo en Test** (verificación no aprobada) envía el código de vuelta para enmienda
- **Escalación** (discrepancia en spec, criterios incorrectos) redirige al Designer
- **Compuerta pending review** asegura que un humano siempre revise antes de marcar como terminado
- **Presupuesto de rondas** (predeterminado 5) previene ciclos infinitos

## Guardrails determinísticos

Aplicados por el CLI, no por el juicio de la IA:

| Guardrail | Qué hace |
|---|---|
| **Verificación de alcance** | Los archivos modificados deben estar dentro de los repos declarados |
| **Snapshot de baseline** | Estado pre-codificación capturado para rollback seguro |
| **Máquina de estados** | Las fases deben proceder en orden legal |
| **Requisito de evidencia** | El Tester debe proporcionar verify.json con salida de comandos |
| **Compuerta de testing** | verify.json + test-report + final-report requeridos |
| **Compuerta de dependencias** | Features con dependencias no cumplidas no pueden iniciar |

## Modo batch

```bash
4x batch plan            # generate dependency-aware execution plan
4x batch run --runner claude  # run all eligible features in order
4x batch stop            # graceful shutdown after current feature
```

## Modelo de permisos

**4x ejecuta agentes de IA en modo no interactivo.** Durante `4x init`, los runners se configuran con banderas que omiten las solicitudes de permisos (`--dangerously-skip-permissions`, `-y`, `approval: full-auto`) para que el ciclo se ejecute de forma autónoma.

Los guardrails determinísticos del CLI (bloqueo de alcance, snapshots de baseline, máquina de estados) proporcionan el límite de seguridad.

**Ejecuta 4x solo en proyectos donde te sientas cómodo con la ejecución autónoma de agentes de IA.**

## Documentación

| Documento | Descripción |
|---|---|
| **[Guía del usuario](docs/guide/es/)** | Documentación completa de uso |
| [Primeros pasos](docs/guide/es/getting-started.md) | Instalación y primera ejecución |
| [Referencia del CLI](docs/guide/es/cli.md) | Todos los comandos y banderas |
| [Conceptos principales](docs/guide/es/concepts.md) | Roles, máquina de estados, protocolo, guardrails |
| [Configuración](docs/guide/es/configuration.md) | Settings, modelos, locale, runners |
| [Runners y plugins](docs/guide/es/runners.md) | Runners soportados y contrato de plugins |
| [Dashboard](docs/guide/es/dashboard.md) | Dashboard multi-proyecto 4x Live |
| [Modo batch](docs/guide/es/batch.md) | Ejecución batch con reconocimiento de dependencias |

## Estructura del proyecto

```
4x/
  cmd/4x/              CLI entry point (Cobra)
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    batch/              Dependency DAG, batch scheduler
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
  plugins/
    claude-code/        Claude Code skill + workflow
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions + workflow
    cursor/             Cursor rules
    embed.go            go:embed plugin files into binary
  dashboard/
    macos/              Swift native app (planned)
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

## Preguntas frecuentes

**P: ¿4x llama a alguna API de LLM directamente?**
No. El CLI es Go puro con cero dependencias de LLM. Los runners manejan toda la interacción con la IA usando las capacidades nativas de su plataforma.

**P: ¿Puedo usar diferentes LLMs para diferentes roles?**
Sí. Configura los modelos por rol en `.4x/settings.json`. Usa Claude para Design, Gemini para Code — cada uno lee los mismos archivos `.4x/`.

**P: ¿En qué se diferencia de Devin / SWE-agent / OpenHands?**
Esos son agentes autónomos que hacen todo de una vez. 4x es un *framework* que estructura la colaboración multi-rol con guardrails determinísticos. Es más parecido a un pipeline de CI para IA que a un solo agente autónomo.

## Historia de origen

4x nació dentro de un sistema de producción llamado DCT (Designer-Coder-Tester) que entregó más de 60 features para una reescritura a gran escala de una plataforma. Los patrones que sobrevivieron — aislamiento de roles, protocolo basado en archivos, verificación determinística de alcance, testing basado en evidencia — se convirtieron en 4x. Las partes que no sobrevivieron — hacks específicos de LLM, suposiciones de contexto compartido, guardrails basados en confianza — fueron deliberadamente excluidas.

## Contribuir

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## Licencia

[MIT](LICENSE)

---

<p align="center">
  <strong>Deja de esperar que tu IA escriba código correcto. Empieza a verificarlo.</strong>
</p>
