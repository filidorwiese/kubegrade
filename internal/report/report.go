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
	"unicode/utf8"

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
	Link     string     `json:"link,omitempty"`
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
			What: f.What, Fix: f.Fix, Link: f.Link, Points: grade.Points(f), Since: f.Since,
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

// TextOptions controls the terminal rendering. Width 0 means no clipping.
type TextOptions struct {
	Color   bool
	Width   int
	Verbose bool // show info findings
}

// WriteText renders the report grouped by category, severe first.
func WriteText(w io.Writer, r Report, opt TextOptions) error {
	paint := func(style, s string) string {
		if !opt.Color || style == "" {
			return s
		}
		return style + s + reset
	}

	fmt.Fprintf(w, "%s  scanned: %s  duration: %.1fs\n\n",
		paint(bold, "kubegrade v"+r.Agent), r.ScannedAt.Format("2006-01-02 15:04:05 UTC"), r.Duration)
	fmt.Fprintf(w, "GRADE  %s (%d)", paint(gradeStyle(r.Grade), fmt.Sprintf("%-3s", r.Grade)), r.Score)
	if r.CappedBy != "" {
		fmt.Fprintf(w, "   capped by: %s", check.CategoryNames[r.CappedBy])
	}
	fmt.Fprint(w, "\n")

	var links []Finding
	for _, c := range r.Categories {
		fmt.Fprintf(w, "\n%s  %s\n", paint(bold, strings.ToUpper(check.CategoryNames[c.ID])), paint(scoreStyle(c.Score), fmt.Sprint(c.Score)))
		var rows []Finding
		hidden := 0
		for _, f := range r.Findings {
			if f.Category != c.ID {
				continue
			}
			if !opt.Verbose && f.Severity == string(check.Info) {
				hidden++
				continue
			}
			rows = append(rows, f)
			if f.Link != "" {
				links = append(links, f)
			}
		}
		// Each category is its own table, so clip per category.
		resW, whatW, fixW := columnWidths(rows, opt.Width)
		// Align first, colour after: escape codes would confuse tabwriter.
		var buf bytes.Buffer
		tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		for _, f := range rows {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", f.Severity, clip(f.Resource, resW), clip(f.What, whatW), clip(f.Fix, fixW))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			if line == "" {
				continue
			}
			sev := strings.Fields(line)[0]
			fmt.Fprintln(w, paint(severityStyle[sev], strings.TrimRight(line, " ")))
		}
		if hidden > 0 {
			fmt.Fprintln(w, paint(dim, fmt.Sprintf("  %d info hidden, -v to show", hidden)))
		}
	}

	if len(links) > 0 {
		fmt.Fprint(w, "\n"+paint(bold, "LINKS")+"\n")
		for _, f := range links {
			fmt.Fprintf(w, "  %s: %s\n", f.Resource, f.Link)
		}
	}
	if len(r.Errors) > 0 {
		fmt.Fprint(w, "\n"+paint(bold+red, "COLLECTOR ERRORS")+"\n")
		for _, e := range r.Errors {
			fmt.Fprintln(w, "  "+strings.TrimSpace(e))
		}
	}
	fmt.Fprintln(w)
	return nil
}

// columnWidths fits resource, what and fix into width. Fixed columns are
// the indent (2), severity (8) and three 2-space gaps. what and resource
// give way first; fix is dropped when there's no room.
func columnWidths(fs []Finding, width int) (resW, whatW, fixW int) {
	for _, f := range fs {
		resW = max(resW, utf8.RuneCountInString(f.Resource))
		whatW = max(whatW, utf8.RuneCountInString(f.What))
		fixW = max(fixW, utf8.RuneCountInString(f.Fix))
	}
	if width <= 0 {
		return resW, whatW, fixW
	}
	const fixed = 2 + 8 + 3*2
	avail := width - fixed
	for _, step := range []struct{ res, what int }{{40, 70}, {40, 50}, {30, 40}, {24, 32}} {
		resW, whatW = min(resW, step.res), min(whatW, step.what)
		if left := avail - resW - whatW; left >= 24 {
			return resW, whatW, min(fixW, left)
		}
	}
	return resW, whatW, 0
}

func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func gradeStyle(letter string) string {
	switch letter {
	case "A+", "A":
		return bold + green
	case "B", "C":
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
