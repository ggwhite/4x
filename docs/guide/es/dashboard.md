# Dashboard 4x Live

Monitoreo en tiempo real de tu ciclo de desarrollo con IA.

## Iniciar el dashboard

```bash
# Start with recent projects
4x live

# Open specific projects
4x live /path/to/project1 /path/to/project2

# Custom port
4x live -p 8080

# Auto-open in browser
4x live -w

# Open macOS native app
4x live -a
```

## Soporte multi-proyecto

El dashboard soporta múltiples proyectos simultáneamente. Sin argumentos de ruta, carga desde `~/.4x/recent-projects.json` (LRU, máximo 20 entradas).

## API del servidor

El dashboard expone endpoints REST y SSE:

### REST

| Endpoint | Método | Descripción |
|---|---|---|
| `/api/tasks` | GET | Listar todos los features |
| `/api/new` | POST | Crear un nuevo feature |
| `/api/run` | POST | Iniciar la ejecución de un feature (crea subproceso `4x run`) |
| `/api/stop` | POST | Detener un feature en ejecución |
| `/api/done` | POST | Marcar feature como terminado |
| `/api/runs` | GET | Listar ejecuciones activas |
| `/api/events/{id}` | GET | Obtener eventos de un feature |
| `/api/messages/{id}` | GET | Obtener mensajes de un feature |
| `/api/logs/{id}` | GET | Listar archivos de log de un feature |
| `/api/logs/{id}/{file}` | GET | Obtener un archivo de log específico |
| `/api/projects` | GET | Listar proyectos registrados |
| `/api/projects` | POST | Agregar un proyecto (soporta `init: true` para inicialización al vuelo) |
| `/api/projects` | DELETE | Eliminar un proyecto |
| `/api/browse` | GET | Selector de carpetas |

### SSE (Server-Sent Events)

| Endpoint | Descripción |
|---|---|
| `/sse/events/{id}` | Transmitir eventos de un feature (polling cada 1 segundo) |
| `/sse/logs/{id}` | Transmitir el archivo de log más reciente de un feature |

### Enrutamiento multi-proyecto

Con múltiples proyectos, los endpoints se prefijan con `/api/project/{project-id}/...` y `/sse/project/{project-id}/...`. El modo de proyecto único usa las rutas sin prefijo para compatibilidad retroactiva.

## Gestión de procesos

El dashboard gestiona subprocesos de runners:

- Respeta `max_concurrent_runs` de la configuración del proyecto
- Captura stdout/stderr como eventos run-output/run-error
- Apagado controlado: SIGTERM → 5 segundos → SIGKILL

## Plataformas

| Plataforma | Estado |
|---|---|
| Web UI (embebida) | Disponible |
| macOS nativa (Swift) | Planificada |
| Electron (Windows/Linux) | Planificada |
