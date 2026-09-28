// Command refresh-data rewrites the embedded EOL yaml tables from
// endoflife.date and Pluto, using the same parsers the agent uses online.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/filidorwiese/kubegrade/internal/data"
)

const plutoURL = "https://raw.githubusercontent.com/FairwindsOps/pluto/master/versions.yaml"

const plutoHeader = "# Vendored from https://github.com/FairwindsOps/pluto (versions.yaml), Apache-2.0.\n" +
	"# Copyright Fairwinds. Do not hand-edit; refresh with hack/refresh-data.sh.\n"

func main() {
	dir := "internal/data"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	t, err := data.Online(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	write(dir, "kubernetes.yaml", t.Kubernetes)
	write(dir, "kernel.yaml", t.Kernel)
	write(dir, "os.yaml", t.OS)

	resp, err := http.Get(plutoURL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		log.Fatalf("pluto: status %d err %v", resp.StatusCode, err)
	}
	must(os.WriteFile(filepath.Join(dir, "k8s-deprecations.yaml"), append([]byte(plutoHeader), body...), 0o644))
	fmt.Println("refreshed", dir)
}

func write(dir, name string, v any) {
	b, err := yaml.Marshal(v)
	must(err)
	must(os.WriteFile(filepath.Join(dir, name), b, 0o644))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
