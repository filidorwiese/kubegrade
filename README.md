# kubegrade-agent

Prototype. One Go binary that runs inside a k3s cluster, scans it every
`--interval`, and prints findings plus a letter grade to stdout. No server,
no UI, no persistence. Nothing leaves the cluster unless `--online` is set,
which only fetches public EOL tables from endoflife.date.

See `PRD.md` for the decisions behind it.

## Run

```sh
# outside the cluster
go run ./cmd/kubegrade-agent --kubeconfig ~/.kube/config --once
go run ./cmd/kubegrade-agent --kubeconfig ~/.kube/config --once --format json --online

# inside the cluster (image built by GitHub Actions on push to main)
kubectl -n kubegrade create secret docker-registry ghcr-token ...   # or copy it in
task deploy
task logs
```

Flags: `--interval` (15m), `--once`, `--format text|json`, `--cluster-name`,
`--online` (off by default; the manifest turns it on), `--kubeconfig`.

`--online` also compares each Helm release against its repo's `index.yaml`.
Releases don't record their repo, so `internal/data/charts.yaml` maps chart
names to repo URLs. Add your charts there; unmapped ones show as info.

## Grading

Five categories of 100 points. Findings deduct info 0, low 3, medium 8,
high 15, critical 30. Overall is the average, capped at one letter above the
worst category, with the score clamped to the top of that band.

A+ 95-100, A 85-94, B 70-84, C 55-69, D 40-54, F 0-39.

## Data

`internal/data/*.yaml` is embedded. `task refresh-data` rewrites it from
endoflife.date and Pluto's `versions.yaml` (Apache-2.0, vendored unchanged
apart from a header) and regenerates `deploy/rbac.yaml`. With `--online`
the same fetch runs at every scan; if it fails the scan is skipped and
retried next interval.

## RBAC

Read-only ClusterRole, explicit resources, no wildcards. The deprecated-API
rules are generated from the Pluto table by `hack/gen-rbac`.

`secrets` list is the sensitive one. The agent lists only secrets matching
`owner=helm` + `type=helm.sh/release.v1` and `type=kubernetes.io/tls`, via
label and field selectors in the code. It parses the certificate or release
payload in memory and never logs secret contents.

## Known limitations

- Sustained-condition durations (CrashLoopBackOff, PVC usage) are measured
  from agent start and reset when the pod restarts. Findings say so.
- "Latest known" Kubernetes version is only as fresh as the embedded table
  unless `--online` is set.
- Single-node k3s makes half the node checks trivially pass. Test against a
  multi-node cluster before believing the Nodes category.
- Deprecated API detection relies on `managedFields` and the last-applied
  annotation; objects written by clients that set neither are invisible.
- Reboot-required, pending OS updates and node-local disk pressure need a
  DaemonSet with host access. Out of scope.
- No CVE data, no image tag lookups. Chart upstream checks need a manual
  entry in `charts.yaml` per chart.
- Flatcar is matched but has no EOL data on endoflife.date.
