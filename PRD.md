# Kubegrade prototype - PRD

Free, open-source CLI. Single Go binary that scans a cluster through kubeconfig, prints findings and a letter grade to stdout. No server, no UI, no persistence. Pivoted from an in-cluster agent on 2026-09-28; deploy manifests, RBAC generator, Dockerfile and CI were removed.

## Decisions (from grilling, 2026-09-28)

| Topic | Decision |
|-------|----------|
| Module | `github.com/filidorwiese/kubegrade` |
| Toolchain | Go 1.26 (pulled in by client-go v0.37); static binary via `task build` |
| Dev loop | `task run -- --online` against the current kubeconfig context |
| CI | none yet |
| Tests | none (prototype) |
| Build order | all 9 steps in one batch |
| Cap rule | overall letter never above the worst category letter; score clamped to top of that band |
| Image checks | Deployments only; skip k3s bundled kube-system workloads |
| Deprecated APIs | Pluto `versions.yaml` vendored; k8s rows only; objects count only when managedFields or last-applied show the deprecated version |
| API server cert | dial the host from the loaded rest config |
| Cluster name | `--cluster-name`, fallback kubeconfig context or `in-cluster` |
| `--online` | default **off**. When on: kubernetes, kernel, OS tables fetched from endoflife.date at startup. Fetch failure aborts scan, logs error, retries next interval. `--online=false` uses embedded yaml |
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
cmd/kubegrade/main.go
internal/collect/    snapshot builders (nodes, pods, deployments, helm, secrets, pvc, certmanager, kubelet, charts)
internal/check/      one file per category
internal/grade/      scoring, letters, cap
internal/report/     text + json
internal/data/       embedded yaml + loader + online fetch + semver
internal/state/      first-seen map
hack/refresh-data.sh
Taskfile.yaml
```

## Out of scope

Tests, in-cluster deployment, DaemonSet, alerting, history, image upstream lookups, CVEs, security posture.
