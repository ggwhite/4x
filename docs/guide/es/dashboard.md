# Dashboard 4x Live

Monitoreo en tiempo real de tu ciclo de desarrollo con IA.

## macOS Gatekeeper

La app 4x Live no está firmada con un certificado de Apple Developer. macOS la bloqueará en el primer lanzamiento.

**Opción A: Eliminar atributo de cuarentena (recomendado)**

```bash
xattr -cr /Applications/4x\ Live.app
```

**Opción B: Permitir desde Configuración del Sistema**

1. Haga doble clic en la app — macOS muestra "no se puede abrir porque no se puede verificar al desarrollador"
2. Abra **Configuración del Sistema → Privacidad y Seguridad**
3. Desplácese hasta la sección **Seguridad** — verá un mensaje sobre la app bloqueada
4. Haga clic en **Abrir de todos modos**, ingrese su contraseña o use Touch ID para confirmar
5. macOS recordará su elección para futuros lanzamientos

## Iniciar el dashboard

```bash
# Iniciar con proyectos recientes
4x live

# Abrir proyectos específicos
4x live /path/to/project1 /path/to/project2

# Puerto personalizado
4x live -p 8080

# Abrir automáticamente en el navegador
4x live -w

# Abrir la app nativa de macOS
4x live -a
```

## Soporte multi-proyecto

El dashboard soporta múltiples proyectos simultáneamente. Sin argumentos de ruta, carga desde `~/.4x/recent-projects.json` (LRU, máximo 20 entradas).

La barra de pestañas de proyectos termina con dos acciones: **Agregar proyecto** (icono de carpeta con signo más) y **Configuración global** (icono de engranaje). El encabezado de la barra lateral lleva el engranaje de **Configuración del proyecto** activo y, junto a él, un botón **Limpiar** (icono de papelera). Al hacer clic en Limpiar se abre un diálogo de confirmación advirtiendo que los features limpiados pierden sus logs detallados, reportes e historial de rondas en el dashboard (las definiciones de features y su estado se preservan); al confirmar se llama a [`POST /api/clean`](#post-apiclean) para todo el proyecto y muestra un toast con el resultado.

## Tarjetas de features

Cada tarjeta de feature muestra etiquetas para su prioridad, dependencias, razón de detención (si el feature se detuvo de forma anormal) y — cuando un [perfil de pipeline](concepts.md#perfiles-de-pipeline) no predeterminado está activo — una **etiqueta de perfil** (ej. `quick`, `normal`). Los features de alta prioridad (P0/P1) obtienen bordes con acento. Las dependencias completadas muestran una marca de verificación verde. Los campos `profile`, `stopReason` y `stopMessage` se llevan en el JSON de `/api/tasks`. `stopReason` es un código de categoría corto (ej. `runner-error`, `guard-fail`, `no-progress`) usado para codificación por color; `stopMessage` es el detalle legible que se muestra debajo de la etiqueta de categoría.

## Formulario de nuevo feature

El modal de **Nuevo feature** es un formulario progresivo. El área básica siempre muestra **Nombre** (obligatorio), **Descripción** (opcional, predeterminado al nombre) y un selector de **Prioridad** (P0-P3 o ninguna). Un botón **Avanzado** revela **ID personalizado** (dejar vacío para auto-generar), **Depende de** (IDs de features separados por comas), **Reglas** (separadas por comas) y una lista dinámica de **Subtareas** (agregar/eliminar filas de id + nombre). Al enviar se hace `POST` a [`/api/new`](#rest); el CLI `4x new` y el dashboard ahora comparten una ruta de creación única (`feature.Create`, ver [Conceptos](concepts.md#creacion-de-features)), por lo que ambos respetan las mismas banderas/campos y generación de IDs.

## DAG de dependencias

El resumen renderiza un grafo de dependencias de todos los features como SVG inline — no se carga ninguna librería de gráficos externa (d3, mermaid, chart.js). Los features se disponen en capas según la profundidad de dependencias; las aristas van de cada feature a los features de los que depende. El color del nodo sigue el estado de fase: verde = done, azul = en ejecución (ejecución activa o fase en progreso como coding/reviewing/testing), gris = pendiente, rojo = blocked / needs-attention. Al hacer clic en un nodo se abre el detalle de ese feature, la misma ruta que al hacer clic en una tarjeta de feature. El grafo se reconstruye desde los datos cacheados de `/api/tasks` en cada ciclo de polling, por lo que los colores se actualizan en vivo a medida que los features avanzan.

## Panel de batch

El resumen también aloja un panel de control de batch respaldado por la [API de control de batch](#control-de-batch). Muestra botones de **Iniciar / Detener / Continuar batch** (Iniciar requiere confirmación antes de lanzar), un indicador de ejecución, la cola programada con progreso por feature (marca de completado, marcador de ejecución o posición en espera) y — cuando un conflicto de merge pausa el batch — una tarjeta de conflicto listando el feature, repo y archivos en conflicto junto con la acción de Continuar batch. El panel se refresca desde `GET /api/batch/status` en el mismo ciclo de polling que el resto del dashboard.

## API del servidor

El dashboard expone endpoints REST y SSE:

Los endpoints de lectura intensiva (`/api/tasks`, `/api/overview`, `/api/projects`, `/api/settings`, ...) se sirven a través de un `*protocol.CachedWorkspace` en lugar de un `*protocol.Workspace` simple. Dado que el servidor es de larga duración, este cache en memoria basado en mtime evita re-parsear cada YAML de feature y `settings.json` en cada solicitud — ver [Cache de lectura del workspace](concepts.md#cache-de-lectura-del-workspace-servidor-del-dashboard). La invalidación del cache es automática: una escritura (vía el dashboard o un runner) cambia el mtime del archivo, por lo que la siguiente lectura re-parsea de forma transparente.

### REST

| Endpoint | Método | Descripción |
|---|---|---|
| `/api/tasks` | GET | Listar todos los features (incluye array `warnings` cuando un feature YAML tiene problemas de formato) |
| `/api/new` | POST | Crear un nuevo feature (acepta `name`, `description`, más opcionales `customId`, `priority`, `depends`, `rules`, `subtasks`) |
| `/api/run` | POST | Iniciar la ejecución de un feature (crea subproceso `4x run`) |
| `/api/stop` | POST | Detener un feature en ejecución |
| `/api/done` | POST | Marcar feature como terminado; fusiona automáticamente el worktree si existe (multi-repo: todo-o-nada) |
| `/api/clean` | POST | Eliminar artefactos del workspace de todos los features limpiables (done/abandoned) del proyecto |
| `/api/runs` | GET | Listar ejecuciones activas |
| `/api/batch/start` | POST | Iniciar una ejecución batch (subproceso `4x batch run`); 409 si hay un conflicto batch sin resolver |
| `/api/batch/stop` | POST | Detener el batch de forma controlada (escribe `.4x/batch-stop`) |
| `/api/batch/continue` | POST | Limpiar la señal de conflicto y reiniciar el batch (después de resolver en el worktree) |
| `/api/batch/status` | GET | Estado de ejecución del batch, cola programada, feature actual y señal de conflicto |
| `/api/events/{id}` | GET | Obtener eventos de un feature |
| `/api/overview/{id}` | GET | Obtener resumen del feature (campos YAML + contenido spec/plan, resuelto vía el `protocol.ResolveDesignDoc` compartido — ver [Resolución de documentos de diseño](concepts.md#resolucion-de-documentos-de-diseno)) |
| `/api/messages/{id}` | GET | Obtener mensajes de un feature |
| `/api/evolve-report` | GET | Último resumen de ronda de `4x evolve` (`.4x/evolve-report.md`); `{content, exists}`, `exists:false` cuando no existe |
| `/api/features/{id}/screenshots` | GET | Obtener capturas de pantalla agrupadas por ronda |
| `/api/features/{id}/screenshots/{filename}` | GET | Servir una imagen de captura de pantalla |
| `/api/logs/{id}` | GET | Listar archivos de log de un feature |
| `/api/logs/{id}/{file}` | GET | Obtener un archivo de log específico |
| `/api/projects` | GET | Listar proyectos registrados |
| `/api/projects` | POST | Agregar un proyecto (soporta `init: true` para inicialización al vuelo) |
| `/api/projects/{id}` | DELETE | Eliminar un proyecto |
| `/api/browse` | GET | Selector de carpetas |
| `/api/settings` | GET | Obtener configuración del proyecto (`.4x/settings.json`) |
| `/api/settings` | PUT | Actualizar configuración del proyecto (valida, respalda, escribe) |
| `/api/user-config` | GET | Obtener configuración del usuario (`~/.4x/settings.json`) |
| `/api/user-config` | PUT | Actualizar configuración del usuario (respalda a `.bak`, luego escribe) |
| `/api/merged-config` | GET | Vista de solo lectura de la configuración efectiva fusionada proyecto + usuario |
| `/api/locales` | GET | Retorna la lista de locales soportados |
| `/api/locales/{lang}` | GET | Retorna el JSON de traducción del idioma correspondiente |
| `/api/supported-runners` | GET | Listar nombres de runners soportados |

#### Respuesta de `POST /api/done`

Retorna HTTP 200 en el caso normal. El campo `status` es `"done"` solo después de que la transición de estado sea exitosa. Si ocurre un conflicto o error de merge, `status` permanece en `"pending-review"`. Campos adicionales indican el resultado del merge:

| Campo | Tipo | Significado |
|---|---|---|
| `merged` | bool | `true` si el branch fue fusionado y el worktree limpiado |
| `merged` | bool | `false` si no existía worktree (transición solo de estado) |
| `merge_conflict` | bool | `true` si el merge tuvo conflictos; worktree preservado |
| `merge_error` | string | Mensaje de error del merge; feature permanece en pending-review |
| `conflicts` | string[] | Lista de archivos en conflicto (solo presente cuando `merge_conflict: true`) |

Después de un conflicto, resuelve los archivos en el worktree y ejecuta `4x merge <id>` para completar.

Si la fase del feature cambia durante el merge (un runner o reconciliador en segundo plano actualizó `state.json` mientras el merge estaba en ejecución), el endpoint retorna **HTTP 409 Conflict** con `{"status":"<currentPhase>","error":"state changed during merge"}` y no realiza la transición a done — esto protege contra sobrescribir un estado más reciente con una instantánea pre-merge obsoleta.

#### `POST /api/clean`

Elimina los artefactos del workspace `.4x/run/{feature-id}/` (logs, `rounds/`, reportes, `state.json`, `events.jsonl`) de **cada** feature limpiable del proyecto en una sola llamada — el mismo conjunto que limpiaría `4x clean`: `done`/`abandoned`, no activo, con directorio de workspace existente. Las definiciones de features (`.4x/features/*.yaml`) se preservan, por lo que los features limpiados siguen apareciendo en listados con su estado final. Ver [Limpieza del workspace](concepts.md#limpieza-del-workspace) para las funciones del protocolo subyacente.

Las solicitudes que no son `POST` retornan **HTTP 405**. Cada feature se limpia independientemente; uno que falle (ej. una condición de carrera lo hizo activo) se omite sin abortar el resto. El handler siempre retorna HTTP 200 con:

| Campo | Tipo | Significado |
|---|---|---|
| `cleaned` | int | Número de features cuyos artefactos fueron eliminados |
| `freed` | int64 | Total de bytes liberados |
| `freed_human` | string | `freed` formateado de forma legible (ej. `38M`) |
| `features` | string[] | IDs de los features limpiados (`[]` cuando no se limpió nada) |

Cuando no hay nada que limpiar, la respuesta es `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`.

#### Control de batch

El dashboard puede manejar una ejecución batch de principio a fin sin volver a la terminal. Un `BatchManager` dedicado (separado del `ProcessManager` por feature) es dueño del único subproceso `4x batch run` de un proyecto — solo un batch puede ejecutarse a la vez.

- **Iniciar** (`POST /api/batch/start`) — la UI confirma primero para evitar lanzamientos accidentales, luego inicia la ejecución. Si `.4x/batch-conflict.json` aún existe, el endpoint retorna **HTTP 409** para que un conflicto obsoleto deba resolverse o continuarse primero. El cuerpo de la solicitud puede llevar `{runner, maxRounds}`; los campos omitidos usan la configuración fusionada proyecto/usuario.
- **Detener** (`POST /api/batch/stop`) — escribe `.4x/batch-stop` para una detención controlada (el batch termina el feature actual, luego sale). **No** mata el subproceso.
- **Continuar** (`POST /api/batch/continue`) — limpia `.4x/batch-conflict.json`, luego reinicia el batch. Usar después de resolver el conflicto en el worktree.
- **Estado** (`GET /api/batch/status`) — retorna el flag de ejecución, la cola programada, el feature actual, la señal de conflicto (o `null`), y `lastReport` (el `.4x/batch-report.json` parseado, u omitido cuando no existe reporte):

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  La cola se construye desde `batch.PlanBatch` y respeta el mismo ordenamiento por dependencia y prioridad que el CLI. El `state` de cada elemento es `done` (feature done / ready-for-review), `running` (ejecución activa que no ha terminado), `error` (blocked / needs-attention) o `waiting`; `position` numera los elementos no terminados (excluye `done` y `error`).

  `lastReport` lleva el reporte de la ejecución batch más reciente (`outcome`, conteos, runner, duración y desglose por feature — ver [Modo batch](batch.md#reporte-de-ejecucion)). Cuando no hay batch en ejecución, el panel lo renderiza como una tarjeta resumen de "último reporte batch" que se expande a detalle por feature; para un outcome `crashed` también muestra el `panicMessage`.

### Pestaña de capturas de pantalla

El detalle del feature incluye una pestaña de **Capturas de pantalla** cuando existen capturas para ese feature. Las capturas se agrupan por ronda, se muestran como miniaturas y se pueden abrir en un lightbox con navegación izquierda/derecha y cierre con ESC.

### SSE (Server-Sent Events)

| Endpoint | Descripción |
|---|---|
| `/sse/events/{id}` | Transmitir eventos de un feature (polling cada 1 segundo) |
| `/sse/logs/{id}` | Transmitir los archivos de log activos del feature (uno o más) |

El flujo de eventos rastrea un desplazamiento de bytes en `events.jsonl` y solo envía las líneas recién agregadas. Si el archivo se **trunca o rota** — por ejemplo `4x transition --to init` reinicia el feature y reescribe `events.jsonl` desde cero — el nuevo tamaño del archivo cae por debajo del desplazamiento rastreado. El flujo detecta esto (`size < lastOffset`), reinicia el desplazamiento a 0 y re-lee todo el archivo desde el principio para que el cliente se recupere en lugar de estancarse silenciosamente. Un tamaño igual al desplazamiento sigue significando "sin contenido nuevo" y se omite.

El flujo de logs (`/sse/logs/{id}`) también rastrea un desplazamiento de bytes y solo envía contenido recién agregado. Para evitar basura por tick, reutiliza un único buffer de lectura fijo de 32KB asignado una vez por conexión en lugar de asignar un nuevo buffer del tamaño de cada delta. En cada tick lee en bucle desde el desplazamiento hasta EOF; un delta mayor de 32KB se divide en varios mensajes SSE, cada uno con el mismo payload `{"file": "...", "content": "..."}`. El cliente agrega contenido a medida que llega, por lo que la división es transparente. Cuando `size <= lastOffset` (sin contenido nuevo) el tick se omite sin abrir el archivo.

Cuando varios roles escriben logs al mismo tiempo — sub-revisores paralelos del deep review, o reviewer + tester concurrentes — el flujo sigue **todos** los logs actualmente activos en lugar de solo el modificado más recientemente. Sin un parámetro de consulta `?file=` rastrea cada log cuyo mtime cae dentro de una ventana reciente (cada uno con su propio desplazamiento), y el campo `file` por mensaje permite al cliente dirigir el contenido al panel correspondiente. Usa `?file=<name>` para fijar el flujo a un solo log.

### Enrutamiento multi-proyecto

Con múltiples proyectos, los endpoints se prefijan con `/api/project/{project-id}/...` y `/sse/project/{project-id}/...`. El modo de proyecto único usa las rutas sin prefijo para compatibilidad retroactiva.

#### Resolución de workspace

Las rutas hoja (`/api/tasks`, `/api/settings`, `/api/run`, `/api/batch/*`, `/sse/events/...`, ...) se definen **una sola vez** en `NewMux` (`internal/server/server.go`). En lugar de vincular un workspace fijo, `NewMux` recibe un `WorkspaceResolver` — una función que, dada la solicitud entrante, retorna el `*protocol.CachedWorkspace` destino, su `*ProcessManager` y su `*BatchManager` (o un error). Cada handler respaldado por datos llama al resolver primero; las rutas que no necesitan ninguno de ellos (`/api/user-config`, `/api/supported-runners`, `/api/locales`, assets estáticos) lo omiten. Esto elimina las ~150 líneas de registro de handlers duplicados que los modos single-project y multi-project llevaban anteriormente por separado.

Dos resolvers respaldan los dos modos:

- **`singleResolver(ws, pm)`** — modo single-project (`server.Start`). Cierra sobre un workspace y siempre retorna el mismo triple `ws`/`pm`/`bm`.
- **`multiResolver(reg)`** — modo multi-project (`NewMultiMux`). La resolución es un flujo de tres pasos:
  1. **Dispatch por prefijo (mux externo).** `NewMultiMux` registra handlers para `/api/project/` y `/sse/project/` que eliminan el prefijo `/api/project/{id}` (o `/sse/project/{id}`), buscan la entrada vía `getEntry(id)` (id desconocido -> **404**), reescriben `r.URL.Path` a la sub-ruta restante, inyectan la entrada resuelta en el `context` de la solicitud y reenvían al handler compartido `NewMux` interno. La eliminación del prefijo debe ocurrir en el mux externo porque `http.ServeMux` selecciona el handler **antes** de ejecutarlo — un `/api/project/{id}/api/tasks` sin eliminar solo coincidiría con la ruta estática `/`.
  2. **Lectura del contexto.** Dentro del handler interno, `multiResolver` primero verifica el contexto de la solicitud para la entrada inyectada en el paso 1 y la retorna directamente cuando está presente.
  3. **Compatibilidad sin prefijo.** Cuando no se inyectó entrada (ruta sin prefijo), usa `reg.Count()`: `0` -> **400** `no projects loaded`, `1` -> ese único proyecto, `>=2` -> **400** `multiple projects loaded — use /api/project/{id}...`.

`NewMultiMux` solo registra los endpoints globales (`/api/projects`, `/api/projects/`, `/api/browse`) más los dos dispatchers por prefijo y un catch-all que reenvía al único `inner := NewMux(multiResolver(reg))` compartido. Agregar un proyecto ya no construye un mux por entrada; `registryEntry` lleva solo `id`/`ws`/`pm`/`bm`.

## Atajos de teclado

| Atajo | Acción |
|---|---|
| `Cmd+K` | Buscar |
| `Cmd+,` | Configuración del proyecto (en proyecto) / Configuración global (en inicio) |
| `Cmd+Shift+,` | Configuración global |
| `Esc` | Cerrar modal actual |

## Gestión de procesos

El dashboard gestiona subprocesos de runners:

- Respeta `max_concurrent_runs` de la configuración del proyecto
- Captura stdout/stderr como eventos run-output/run-error
- Apagado controlado: SIGTERM -> 5 segundos -> SIGKILL

Cuando un subproceso de runner sale, el servidor marca el feature como inactivo (`Active=false`, `StopReason=process-exit`). Esto está protegido contra una condición de carrera: un runner puede escribir su propio `state.json` final (ej. `pending-review`) justo antes de salir. El servidor registra la hora de salida del proceso y, antes de sobrescribir, re-lee el estado — si `state.json` fue actualizado **en o después de** la hora de salida (`UpdatedAt >= endTime`), el estado final del runner se mantiene y la escritura de inactivo se omite. Esto evita que el servidor revierta una fase recién escrita o sobrescriba su `StopReason` con una instantánea obsoleta en memoria.

## Frontend web compartido

La UI del dashboard (HTML/CSS/JS + JSON de locale) vive en una única fuente de verdad en `dashboard/web/` y está embebida en el binario `4x` vía `dashboard/web/embed.go` (`web.Assets embed.FS`). El servidor Go (`internal/server/server.go`, `internal/server/multi.go`) sirve assets estáticos y archivos de locale directamente desde `web.Assets`, por lo que el mismo frontend respalda cada shell de plataforma — la web UI servida por Go, el WKWebView de macOS y el webview de Tauri. No hay una copia de UI por plataforma que mantener sincronizada.

## Plataformas

| Plataforma | Shell | Empaquetado |
|---|---|---|
| Web UI (embebida) | El servidor Go sirve `web.Assets` | `4x live` |
| macOS nativa | Swift WKWebView, auto-lanza el servidor `4x live` bundleado | `.dmg` universal (`make package-macos`) |
| Windows | Tauri v2 webview, sidecar `4x` | `.msi` (`dashboard/tauri`) |
| Linux | Tauri v2 webview, sidecar `4x` | `.AppImage` (`dashboard/tauri`) |

Todos los shells de escritorio cargan el mismo frontend `dashboard/web/` sobre `http://localhost:<port>` respaldado por el servidor `4x` embebido. La matriz CI en `.github/workflows/desktop.yml` cross-compila el binario `4x` por plataforma y produce los artefactos `.dmg` / `.msi` / `.AppImage`.
