// Command refresh-data re-vendors Pluto's versions.yaml and the endoflife.date
// products into internal/data. The EOL files are the offline fallback; the
// release workflow runs this before building so every release ships current
// tables.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const plutoURL = "https://raw.githubusercontent.com/FairwindsOps/pluto/master/versions.yaml"

const plutoHeader = "# Vendored from https://github.com/FairwindsOps/pluto (versions.yaml), Apache-2.0.\n" +
	"# Copyright Fairwinds. Do not hand-edit; refresh with hack/refresh-data.sh.\n"

const eolBase = "https://endoflife.date/api/"

var eolProducts = []string{"kubernetes", "linux", "ubuntu", "debian", "amazon-linux"}

func main() {
	dir := "internal/data"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	write(filepath.Join(dir, "k8s-deprecations.yaml"), append([]byte(plutoHeader), get(plutoURL)...))

	for _, p := range eolProducts {
		body := get(eolBase + p + ".json")
		// A shape change upstream must fail here, not in a user's scan.
		var cycles []map[string]any
		if err := json.Unmarshal(body, &cycles); err != nil || len(cycles) < 3 {
			log.Fatalf("endoflife.date %s: unexpected payload (%v)", p, err)
		}
		write(filepath.Join(dir, "eol", p+".json"), body)
	}
	write(filepath.Join(dir, "eol", "DATE"), []byte(time.Now().UTC().Format("2006-01-02")+"\n"))
}

func get(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		log.Fatalf("%s: status %d err %v", url, resp.StatusCode, err)
	}
	return body
}

func write(path string, b []byte) {
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("refreshed", path)
}
