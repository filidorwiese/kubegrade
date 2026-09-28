# Kubegrade agent prototype - PRD

Single Go binary running in-cluster on k3s. Scans on a timer, prints findings and a letter grade to stdout. No server, no UI, no persistence.

## Decisions (from grilling, 2026-09-28)

| Topic | Decision |
|-------|----------|
| Module | `github.com/filidorwiese/kubegrade` |
| Toolchain | Go from Debian trixie apt (1.24); client-go newest stable, server version detected at runtime |
| Dev loop | push to GitHub -> Actions builds amd64 image -> `ghcr.io/filidorwiese/kubegrade-agent` (`:latest`, `:sha`) -> `task deploy` -> `task logs` |
| Registry | private; pull secret `ghcr-token`, copied into ns `kubegrade` by hand |
| CI | build + push only, no test gate |
| Tests | none (prototype) |
| Build order | all 9 steps in one batch |
| Cap rule | overall letter never above the worst category letter; score clamped to top of that band |
| Image checks | Deployments only; skip k3s bundled kube-system workloads |
| Deprecated APIs | Pluto `versions.yaml` vendored; k8s rows only; ClusterRole list rules generated from table |
| API server cert | dial the host from the loaded rest config |
| Cluster name | `--cluster-name`, fallback kubeconfig context or `in-cluster` |
| `--online` | default **off**; manifest passes `--online`. When on: kubernetes, kernel, OS tables fetched from endoflife.date at startup. Fetch failure aborts scan, logs error, retries next interval. `--online=false` uses embedded yaml |
| Chart upstream | `--online` only: `internal/data/charts.yaml` maps chart name to repo, fetch `index.yaml`, semver compare. Medium if major behind (fix points at the chart sources URL for the changelog), low per minor (max 3), info for patch. Unmapped chart is info. Repo fetch failure is a collector error, not a scan abort |
| Image tag lookups | not built |
| cert-manager | implemented behind CRD discovery, not present on target cluster |

## Grading

Five categories, 100 points each. Severity points: info 0, low 3, medium 8, high 15, critical 30. Category floor 0.
Overall = average, then capped at the worst category letter, score clamped to band top.
Letters: A+ 95-100, A 85-94, B 70-84, C 55-69, D 40-54, F 0-39.

## Checks

See handover doc (checks table per category). Check IDs are stable:

- control-plane: `k8s-version-eol`, `k8s-version-behind`, `k8s-data-stale`, `k8s-api-deprecated`
- workloads: `helm-status`, `helm-revisions`, `chart-outdated`, `chart-unmapped`, `image-tag-latest`, `image-no-digest`
- nodes: `kubelet-skew`, `kernel-eol`, `os-eol`, `node-drift`, `node-info`, `node-notready`
- certificates: `apiserver-cert-expiry`, `tls-secret-expiry`, `certmanager-not-ready`, `certmanager-issuing-failed`
- sustained: `pod-crashloop`, `pod-pending`, `deploy-unavailable`, `pvc-usage`

## Layout

```
cmd/kubegrade-agent/main.go
internal/collect/    snapshot builders (nodes, pods, deployments, helm, secrets, pvc, certmanager, kubelet)
internal/check/      one file per category
internal/grade/      scoring, letters, cap
internal/report/     text + json
internal/data/       embedded yaml + loader + online fetch
internal/state/      first-seen map
deploy/              namespace, rbac, deployment
hack/refresh-data.sh
Taskfile.yaml
.github/workflows/image.yaml
```

## Out of scope

Tests, DaemonSet, alerting, history, image upstream lookups, CVEs, security posture.
