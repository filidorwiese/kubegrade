# kubegrade

Free, open-source CLI that scans a Kubernetes cluster through your kubeconfig
and prints findings plus a letter grade. Built for k3s, works on any cluster.
Read-only. Nothing leaves your machine unless `--online` is set, which fetches
public EOL tables from endoflife.date and Helm repo `index.yaml` files.

See `PRD.md` for the decisions behind it.

## Run

```sh
go run ./cmd/kubegrade
go run ./cmd/kubegrade --online --format json
go run ./cmd/kubegrade --kubeconfig ~/.kube/other
```

One scan per invocation. Flags: `--format text|json`, `--cluster-name`
(defaults to the current context), `--online`, `--kubeconfig`
(defaults to `$KUBECONFIG` or `~/.kube/config`).

`task build` puts a static binary in `bin/kubegrade`.

## Grading

Four categories of 100 points: control plane, workloads, nodes, sustained
conditions. Findings deduct info 0, low 3, medium 8,
high 15, critical 30. Overall is the average, capped at the worst category's
letter, with the score clamped to the top of that band.

A+ 95-100, A 85-94, B 70-84, C 55-69, D 40-54, F 0-39.

## Data

`internal/data/*.yaml` is embedded. `task refresh-data` rewrites it from
endoflife.date and Pluto's `versions.yaml` (Apache-2.0, vendored unchanged
apart from a header). With `--online` the same fetch runs at startup; if it
fails the run exits with an error.

`--online` also compares each Helm release against its repo's `index.yaml`.
Releases don't record their repo, so `internal/data/charts.yaml` maps chart
names to repo URLs. Add your charts there; unmapped ones show as info.

## Permissions

The kubeconfig user needs read access, cluster-wide, to: nodes, nodes/proxy,
pods, namespaces, persistentvolumeclaims, secrets, deployments, replicasets,
and list on whatever deprecated API groups the Pluto table names.
Cluster-admin covers it.

`secrets` is the sensitive one. The tool lists only secrets matching
`owner=helm` + `type=helm.sh/release.v1`, via label and field selectors in
the code. It decodes the release payload in memory and never prints secret
contents.

## Known limitations

- A single run cannot measure how long a CrashLoopBackOff or a full PVC has
  lasted. Severity uses restart count and fill percentage instead. Pending
  pods, unavailable deployments and NotReady nodes use real API timestamps.
- "Latest known" Kubernetes version is only as fresh as the embedded table
  unless `--online` is set.
- Single-node clusters make half the node checks trivially pass.
- Deprecated API detection relies on `managedFields` and the last-applied
  annotation; objects written by clients that set neither are invisible.
- Reboot-required, pending OS updates and node-local disk pressure need
  host access. Out of scope.
- No CVE data, no image tag lookups. Chart upstream checks need a manual
  entry in `charts.yaml` per chart.
- Flatcar is matched but has no EOL data on endoflife.date.
