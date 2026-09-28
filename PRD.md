# Kubegrade prototype - PRD

Free, open-source CLI. Single Go binary that scans a cluster through kubeconfig once, prints findings and a letter grade to stdout. No loop, no server, no UI, no persistence. Pivoted from an in-cluster agent on 2026-09-28; deploy manifests, RBAC generator, Dockerfile and CI were removed.

## Decisions (from grilling, 2026-09-28)

| Topic | Decision |
|-------|----------|
| Module | `github.com/filidorwiese/kubegrade` |
| Toolchain | Go 1.26 (pulled in by client-go v0.37); static binary via `task build` |
| Dev loop | `task run` against the current kubeconfig context |
| CI | none yet |
| Tests | none (prototype) |
| Build order | all 9 steps in one batch |
| Cap rule | overall letter never above the worst category letter; score clamped to top of that band |
| Image checks | Deployments only; skip k3s bundled kube-system workloads |
| Deprecated APIs | Pluto `versions.yaml` vendored; k8s rows only; objects count only when managedFields or last-applied show the deprecated version |
| Cluster name | `--cluster-name`, fallback kubeconfig context or `in-cluster` |
| Online | always. Kubernetes, kernel, OS tables fetched from endoflife.date at startup; fetch failure exits with an error. `--online` flag and embedded EOL yaml removed 2026-09-28. Pluto table and chart mapping stay embedded |
| Colour | text output coloured by severity and score when stdout is a tty; `--no-color` or `NO_COLOR` disables. |
| Progress | single-line bar on stderr, only when stderr is a tty; stdout stays clean for JSON |
| Chart lookups | 8 concurrent workers; Artifact Hub HTTP 429 stops further lookups and is reported as a collector error, no cache yet |
| Run mode | single scan per invocation; `--interval`/`--once` and the in-memory first-seen store removed 2026-09-28 |
| Certificates | category removed 2026-09-28 (API server cert, TLS secrets, cert-manager) |
| Chart upstream | Artifact Hub search by exact chart name, disambiguated by Chart.yaml `home`/`sources`, repo-name hint, official flag; else best by verified/stars and flagged guessed. `internal/data/charts.yaml` overrides with a repo URL (fetch `index.yaml`). Semver compare. Medium if major behind (fix points at the chart sources URL for the changelog), low per minor (max 3), info for patch. Unmapped chart is info. Repo fetch failure is a collector error, not a scan abort |
| Image tag lookups | not built |

## Grading

Three categories, 100 points each: versions, hygiene, health (regrouped 2026-09-28 from control-plane/workloads/nodes/sustained). Severity points: info 0, low 3, medium 8, high 15, critical 30. Category floor 0.
Overall = average, then capped at the worst category letter, score clamped to band top.
Letters: A+ 95-100, A 85-94, B 70-84, C 55-69, D 40-54, F 0-39.

## Checks

Check IDs are stable:

- versions: `k8s-version-eol`, `k8s-version-behind`, `kernel-eol`, `os-eol`, `kubelet-skew`, `chart-outdated`, `chart-unresolved`, `node-info`
- hygiene: `k8s-api-deprecated`, `image-tag-latest`, `image-no-digest`, `helm-revisions`, `node-drift`
- health: `helm-status`, `pod-crashloop` (severity by restart count), `pod-restarts` (5+ restarts per workload: low, medium if the last one was within 24h; both crash checks show the dominant last-termination reason such as OOMKilled), `pod-pending`, `deploy-unavailable`, `node-notready`, `pvc-usage` (medium at 90%, high at 95%)

## Layout

```
cmd/kubegrade/main.go
internal/collect/    snapshot builders (nodes, pods, deployments, helm, pvc, kubelet, charts)
internal/check/      checks grouped by data source; category set per check
internal/grade/      scoring, letters, cap
internal/report/     text + json
internal/progress/   stderr progress bar
internal/data/       embedded Pluto table + charts.yaml override, endoflife.date fetch, semver
hack/refresh-data.sh re-vendors Pluto
Taskfile.yaml
```

## Out of scope

Tests, in-cluster deployment, DaemonSet, certificates, alerting, history, image upstream lookups, CVEs, security posture.
