# 4x Design — Batch Mode

> Extracted from design.md §8

---

Batch mode schedules and runs multiple features automatically, respecting inter-feature dependencies.

## 8.1 Dependency Graph

Dependencies are declared in `features/*.yaml` under the `dependencies` key (list of feature IDs). The batch planner builds a directed acyclic graph (DAG).

If cycles are detected, `4x batch plan` fails with an error listing the cycle.

## 8.2 Union-Find Grouping

Features are grouped into independent clusters using Union-Find on their dependency graph. Clusters with no shared repos or dependencies can run fully in parallel.

## 8.3 Hub and Leaf Repos

Within a cluster:

- **Hub repo:** A repo depended on by 2 or more features in the same batch. Features touching a hub repo are serialized within that cluster.
- **Leaf repo:** A repo touched by only one feature. Leaf-only features may run concurrently.

## 8.4 Bridge Detection

A **bridge** in the dependency graph is an edge whose removal disconnects the graph. Bridge features (features that are the sole dependency path between two parts of the graph) are scheduled conservatively: they must complete before any downstream cluster begins.

## 8.5 Chain Scheduling

Within a cluster, features are sorted topologically and run in chains. A chain is a maximal sequence of features where each depends on the previous.

The maximum chain length is configurable (`batch.max_chain_length` in `settings.json`, default 4). Chains longer than the limit are split: the first segment runs, must reach `done`, then the next segment is unlocked.

This prevents long-running batch jobs from blocking the entire workspace on a single failing feature.

## 8.6 Scheduling Algorithm

```
batch plan:
  1. Build DAG from all features with status != done.
  2. Detect cycles → error if found.
  3. Union-Find to identify independent clusters.
  4. For each cluster:
     a. Topological sort.
     b. Identify hub repos.
     c. Detect bridges.
     d. Split into chains (max_chain_length).
     e. Emit a schedule: ordered list of (feature-id, can-start-after, slot).
  5. Write schedule to .4x/batch-plan.json.

batch next:
  1. Read batch-plan.json.
  2. Find features where all dependencies are done and a parallel slot is free.
  3. Start the next eligible feature.
```

## 8.7 `batch-plan.json` Format

```jsonc
{
  "generatedAt": "2026-06-10T08:00:00Z",
  "clusters": [
    {
      "id": "cluster-0",
      "features": ["user-authentication", "rest-api-for-todo-items"],
      "chains": [
        ["user-authentication", "rest-api-for-todo-items"]
      ]
    }
  ],
  "schedule": [
    {
      "featureId": "user-authentication",
      "slot": 0,
      "canStartAfter": []
    },
    {
      "featureId": "rest-api-for-todo-items",
      "slot": 0,
      "canStartAfter": ["user-authentication"]
    }
  ]
}
```
