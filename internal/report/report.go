// Package report renders a scan as text or JSON. The JSON shape is flat on
// purpose; the future service keys on finding IDs.
package report

import (
	"bytes"
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

// ANSI styles; applied only when WriteText is called with color=true.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
)

var severityStyle = map[string]string{
	"critical": bold + red,
	"high":     red,
	"medium":   yellow,
	"low":      cyan,
	"info":     dim,
}

func gradeStyle(letter string) string {
	switch letter {
	case "A+", "A":
		return bold + green
	case "B":
		return bold + yellow
	case "C":
		return bold + yellow
	default:
		return bold + red
	}
}

func scoreStyle(score int) string {
	switch {
	case score >= 85:
		return green
	case score >= 70:
		return yellow
	default:
		return red
	}
}

func WriteText(w io.Writer, r Report, color bool) error {
	paint := func(style, s string) string {
		if !color || style == "" {
			return s
		}
		return style + s + reset
	}

	fmt.Fprintf(w, "%s  cluster: %s  scanned: %s  duration: %.1fs\n\n",
		paint(bold, "kubegrade v"+r.Agent), r.Cluster, r.ScannedAt.Format("2006-01-02 15:04:05 UTC"), r.Duration)
	fmt.Fprintf(w, "GRADE  %s (%d)", paint(gradeStyle(r.Grade), fmt.Sprintf("%-3s", r.Grade)), r.Score)
	if r.CappedBy != "" {
		fmt.Fprintf(w, "   capped by: %s", check.CategoryNames[r.CappedBy])
	}
	fmt.Fprint(w, "\n\n")

	for _, c := range r.Categories {
		fmt.Fprintf(w, "%-12s %s   %s\n", check.CategoryNames[c.ID],
			paint(scoreStyle(c.Score), fmt.Sprintf("%3d", c.Score)), c.Summary)
	}

	fmt.Fprint(w, "\n"+paint(bold, "FINDINGS")+" (ordered by points)\n\n")
	// Align first, colour after: escape codes would confuse tabwriter.
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
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
	for _, line := range strings.SplitAfter(buf.String(), "\n") {
		if line == "" {
			continue
		}
		sev := line[1:strings.IndexByte(line, ']')]
		fmt.Fprint(w, paint(severityStyle[sev], strings.TrimRight(line, "\n"))+"\n")
	}
	if len(r.Errors) > 0 {
		fmt.Fprint(w, "\n"+paint(bold+red, "COLLECTOR ERRORS")+"\n\n")
		for _, e := range r.Errors {
			fmt.Fprintln(w, "  "+strings.TrimSpace(e))
		}
	}
	fmt.Fprintln(w)
	return nil
}
