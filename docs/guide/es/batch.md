# Modo batch

Ejecutar múltiples features en orden con reconocimiento de dependencias.

## Flujo de trabajo

```bash
# 1. Generate execution plan
4x batch plan

# 2. Check what's next
4x batch next

# 3. Run all eligible features
4x batch run --runner claude

# 4. Gracefully stop (finishes current feature)
4x batch stop
```

## Planificación

`4x batch plan` analiza todos los features no terminados y genera `.4x/batch-plan.json`:

1. **DAG de dependencias** — construye un grafo dirigido a partir de los campos `depends` de los features
2. **Detección de ciclos** — produce error si existen dependencias circulares
3. **Agrupación Union-Find** — agrupa features que comparten repositorios
4. **Ordenamiento topológico** — ordena features dentro de cada cluster
5. **Programación en cadena** — divide cadenas de dependencias largas (longitud máxima configurable con `--max-chain`)

```bash
# Preview the plan
4x batch plan --dry-run

# Limit chain length
4x batch plan --max-chain 3
```

Ejemplo de salida:

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## Ejecución

`4x batch run` ejecuta features secuencialmente en orden de dependencias:

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- Usa la estrategia de commits `"never"` (tú haces commit manualmente después de la revisión)
- Verifica el archivo `.4x/batch-stop` entre features
- Omite features cuyas dependencias no están terminadas
- Reporta progreso después de cada feature

## Detención

```bash
4x batch stop
```

Crea un archivo señal `.4x/batch-stop`. El batch termina el feature actual y luego sale de forma controlada.

## Verificar progreso

```bash
# See which feature is next
4x batch next

# Overview of all features
4x status
```
