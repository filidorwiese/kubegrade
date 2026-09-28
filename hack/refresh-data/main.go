// Command refresh-data re-vendors Pluto's versions.yaml into internal/data.
// EOL tables are fetched live at runtime and need no refresh.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const plutoURL = "https://raw.githubusercontent.com/FairwindsOps/pluto/master/versions.yaml"

const plutoHeader = "# Vendored from https://github.com/FairwindsOps/pluto (versions.yaml), Apache-2.0.\n" +
	"# Copyright Fairwinds. Do not hand-edit; refresh with hack/refresh-data.sh.\n"

func main() {
	dir := "internal/data"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	resp, err := http.Get(plutoURL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		log.Fatalf("pluto: status %d err %v", resp.StatusCode, err)
	}
	out := filepath.Join(dir, "k8s-deprecations.yaml")
	if err := os.WriteFile(out, append([]byte(plutoHeader), body...), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("refreshed", out)
}
