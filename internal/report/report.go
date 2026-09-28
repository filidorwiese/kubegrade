// Package report renders a scan as text or JSON. The JSON shape is flat on
// purpose; the future service keys on finding IDs.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/filidorwiese/kubegrade/internal/check"
	"github.com/filidorwiese/kubegrade/internal/grade"
)

type Report struct {
	Agent      string     `json:"agent"`
	Cluster    string     `json:"cluster"`
	ScannedAt  time.Time  `json:"scanned_at"`
	Duration   float64    `json:"duration_seconds"`
	Grade      string     `json:"grade"`
	Score      int        `json:"score"`
	CappedBy   string     `json:"capped_by,omitempty"`
	Categories []Category `json:"categories"`
	Findings   []Finding  `json:"findings"`
	Errors     []string   `json:"errors,omitempty"`
}

type Category struct {
	ID      string `json:"id"`
	Score   int    `json:"score"`
	Summary string `json:"summary"`
}

type Finding struct {
	ID       string     `json:"id"`
	Category string     `json:"category"`
	Severity string     `json:"severity"`
	Resource string     `json:"resource"`
	What     string     `json:"what"`
	Fix      string     `json:"fix"`
	Points   int        `json:"points"`
	Since    *time.Time `json:"since"`
}

type Input struct {
	Agent, Cluster string
	ScannedAt      time.Time
	Duration       time.Duration
	Findings       []check.Finding
	Result         grade.Result
	Errors         []string
}

func Build(in Input) Report {
	r := Report{
		Agent: in.Agent, Cluster: in.Cluster, ScannedAt: in.ScannedAt.UTC(),
		Duration: in.Duration.Seconds(), Grade: in.Result.Grade, Score: in.Result.Score,
		CappedBy: in.Result.CappedBy, Errors: in.Errors,
	}
	for _, c := range in.Result.Categories {
		r.Categories = append(r.Categories, Category{ID: c.ID, Score: c.Score, Summary: c.Summary})
	}
	for _, f := range in.Findings {
		r.Findings = append(r.Findings, Finding{
			ID: f.ID, Category: f.Category, Severity: string(f.Severity), Resource: f.Resource,
			What: f.What, Fix: f.Fix, Points: grade.Points(f), Since: f.Since,
		})
	}
	// Costliest first, then category order, then resource for stable output.
	catOrder := map[string]int{}
	for i, c := range check.Categories {
		catOrder[c] = i
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if catOrder[a.Category] != catOrder[b.Category] {
			return catOrder[a.Category] < catOrder[b.Category]
		}
		return a.Resource < b.Resource
	})
	return r
}

func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func WriteText(w io.Writer, r Report) error {
	fmt.Fprintf(w, "kubegrade v%s  cluster: %s  scanned: %s  duration: %.1fs\n\n",
		r.Agent, r.Cluster, r.ScannedAt.Format("2006-01-02 15:04:05 UTC"), r.Duration)
	fmt.Fprintf(w, "GRADE  %-3s (%d)", r.Grade, r.Score)
	if r.CappedBy != "" {
		fmt.Fprintf(w, "   capped by: %s", check.CategoryNames[r.CappedBy])
	}
	fmt.Fprint(w, "\n\n")

	for _, c := range r.Categories {
		fmt.Fprintf(w, "%-21s%3d   %s\n", check.CategoryNames[c.ID], c.Score, c.Summary)
	}

	fmt.Fprint(w, "\nFINDINGS (ordered by points)\n\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range r.Findings {
		pts := ""
		if f.Points > 0 {
			pts = fmt.Sprintf("-%d", f.Points)
		}
		fmt.Fprintf(tw, "[%s]\t%s\t%s\t%s\t%s\t%s\n", f.Severity, f.Category, f.Resource, f.What, f.Fix, pts)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Errors) > 0 {
		fmt.Fprint(w, "\nCOLLECTOR ERRORS\n\n")
		for _, e := range r.Errors {
			fmt.Fprintln(w, "  "+strings.TrimSpace(e))
		}
	}
	fmt.Fprintln(w)
	return nil
}
