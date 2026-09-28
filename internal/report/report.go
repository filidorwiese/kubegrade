// Package report renders a scan as text or JSON. The JSON shape is flat on
// purpose; the future service keys on finding IDs.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
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
	Grade   string `json:"grade"`
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
		r.Categories = append(r.Categories, Category{ID: c.ID, Grade: c.Grade, Score: c.Score, Summary: c.Summary})
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

// Block letters, 5 rows by 6 columns, for the grade banner.
var glyphs = map[string][5]string{
	"A": {" ████ ", "██  ██", "██████", "██  ██", "██  ██"},
	"B": {"█████ ", "██  ██", "█████ ", "██  ██", "█████ "},
	"C": {" █████", "██    ", "██    ", "██    ", " █████"},
	"D": {"█████ ", "██  ██", "██  ██", "██  ██", "█████ "},
	"F": {"██████", "██    ", "█████ ", "██    ", "██    "},
	"+": {"      ", "  ██  ", "██████", "  ██  ", "      "},
}

// WriteText renders the grade banner with the category breakdown beside
// it, then one table of all findings sorted by category and severity.
func WriteText(w io.Writer, r Report, opt TextOptions) error {
	paint := func(style, s string) string {
		if !opt.Color || style == "" {
			return s
		}
		return style + s + reset
	}

	fmt.Fprintf(w, "%s  scanned: %s  duration: %.1fs\n\n",
		paint(bold, "kubegrade v"+r.Agent), r.ScannedAt.Format("2006-01-02 15:04:05 UTC"), r.Duration)

	// Banner: block letter left, breakdown right.
	var banner [5]string
	for _, ch := range strings.Split(r.Grade, "") {
		g := glyphs[ch]
		for i := range banner {
			banner[i] += g[i] + " "
		}
	}
	bannerW := utf8.RuneCountInString(banner[0])
	var side []string
	for _, c := range r.Categories {
		side = append(side, fmt.Sprintf("%-9s %s  %s", check.CategoryNames[c.ID], paint(gradeStyle(c.Grade), fmt.Sprintf("%-2s", c.Grade)), summarise(r.Findings, c.ID)))
	}
	if r.CappedBy != "" {
		side = append(side, "", paint(dim, "capped by "+check.CategoryNames[r.CappedBy]))
	}
	for i := range banner {
		line := "  " + paint(gradeStyle(r.Grade), banner[i])
		if i < len(side) {
			line += "   " + side[i]
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
	for i := len(banner); i < len(side); i++ {
		fmt.Fprintln(w, strings.Repeat(" ", bannerW+5)+side[i])
	}
	fmt.Fprintln(w)

	// Findings table.
	var rows []Finding
	hidden := 0
	var links []Finding
	for _, f := range r.Findings {
		if !opt.Verbose && f.Severity == string(check.Info) {
			hidden++
			continue
		}
		rows = append(rows, f)
		if f.Link != "" {
			links = append(links, f)
		}
	}
	catOrder := map[string]int{}
	for i, c := range check.Categories {
		catOrder[c] = i
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if catOrder[rows[i].Category] != catOrder[rows[j].Category] {
			return catOrder[rows[i].Category] < catOrder[rows[j].Category]
		}
		return rows[i].Points > rows[j].Points
	})

	if len(rows) > 0 {
		resW, whatW, fixW := columnWidths(rows, opt.Width)
		headers := []string{"Category", "Severity", "Resource", "Finding", "Fix"}
		widths := []int{8, 8, max(resW, 8), max(whatW, 7), max(fixW, 3)}
		var fixes []Finding
		if fixW == 0 {
			headers, widths = headers[:4], widths[:4]
		}
		cells := func(f Finding) [][]string {
			c := [][]string{{f.Category}, {f.Severity}, wrap(f.Resource, resW), wrap(f.What, whatW)}
			if fixW > 0 {
				c = append(c, wrap(f.Fix, fixW))
			} else if f.Fix != "" {
				fixes = append(fixes, f)
			}
			return c
		}
		rule := func(l, m, r string) string {
			parts := make([]string, len(widths))
			for i, n := range widths {
				parts[i] = strings.Repeat("─", n+2)
			}
			return paint(dim, l+strings.Join(parts, m)+r)
		}
		// row renders one table row; cells may span several lines.
		row := func(c [][]string, style func(i int, s string) string) string {
			height := 0
			for _, lines := range c {
				height = max(height, len(lines))
			}
			var b strings.Builder
			for ln := 0; ln < height; ln++ {
				b.WriteString(paint(dim, "│"))
				for i, lines := range c {
					s := ""
					if ln < len(lines) {
						s = lines[ln]
					}
					pad := widths[i] - utf8.RuneCountInString(s)
					b.WriteString(" " + style(i, s) + strings.Repeat(" ", pad) + " " + paint(dim, "│"))
				}
				b.WriteString("\n")
			}
			return strings.TrimRight(b.String(), "\n")
		}
		single := func(cells []string) [][]string {
			out := make([][]string, len(cells))
			for i, c := range cells {
				out[i] = []string{c}
			}
			return out
		}
		fmt.Fprintln(w, rule("┌", "┬", "┐"))
		fmt.Fprintln(w, row(single(headers), func(_ int, s string) string { return paint(bold, s) }))
		fmt.Fprintln(w, rule("├", "┼", "┤"))
		for i, f := range rows {
			if i > 0 {
				fmt.Fprintln(w, rule("├", "┼", "┤"))
			}
			sev := f.Severity
			fmt.Fprintln(w, row(cells(f), func(i int, s string) string {
				if i == 1 {
					return paint(severityStyle[sev], s)
				}
				return s
			}))
		}
		fmt.Fprintln(w, rule("└", "┴", "┘"))
		if len(fixes) > 0 {
			fmt.Fprintln(w, "\n"+paint(bold, "Fixes"))
			for _, f := range fixes {
				fmt.Fprintf(w, "  %s: %s\n", f.Resource, f.Fix)
			}
		}
	} else {
		fmt.Fprintln(w, "  🙌 nothing to fix")
	}
	if hidden > 0 {
		fmt.Fprintln(w, paint(dim, fmt.Sprintf("  %d info hidden, -v to show", hidden)))
	}

	if len(links) > 0 {
		fmt.Fprint(w, "\n"+paint(bold, "External links")+"\n")
		for _, f := range links {
			fmt.Fprintf(w, "  %s: %s\n", f.Resource, f.Link)
		}
	}
	if len(r.Errors) > 0 {
		fmt.Fprint(w, "\n"+paint(bold+red, "Collector errors")+"\n")
		for _, e := range r.Errors {
			fmt.Fprintln(w, "  "+strings.TrimSpace(e))
		}
	}
	fmt.Fprintln(w)
	return nil
}

// summarise counts non-info findings of a category by severity.
func summarise(fs []Finding, category string) string {
	counts := map[string]int{}
	for _, f := range fs {
		if f.Category == category && f.Severity != string(check.Info) {
			counts[f.Severity]++
		}
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	if len(parts) == 0 {
		return "no findings"
	}
	return strings.Join(parts, ", ")
}

// columnWidths fits resource, what and fix into width. The box costs
// 16 columns of borders and padding plus the two 8-wide fixed columns.
// Resource and fix get modest caps; the finding column takes the rest.
// Fix is dropped below the table when there is no room for it.
func columnWidths(fs []Finding, width int) (resW, whatW, fixW int) {
	for _, f := range fs {
		resW = max(resW, utf8.RuneCountInString(f.Resource))
		whatW = max(whatW, utf8.RuneCountInString(f.What))
		fixW = max(fixW, utf8.RuneCountInString(f.Fix))
	}
	if width <= 0 {
		return resW, whatW, fixW
	}
	const fixed = 16 + 8 + 8
	avail := width - fixed
	resW = min(resW, 32)
	fixW = min(fixW, 28)
	if rest := avail - resW - fixW; rest >= 30 {
		return resW, min(whatW, rest), fixW
	}
	resW = min(resW, 24)
	return resW, max(min(whatW, avail-resW), 16), 0
}

// wrap breaks s into lines of at most n runes on spaces; a single word
// longer than n is cut. n <= 0 means no wrapping.
func wrap(s string, n int) []string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return []string{s}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		for utf8.RuneCountInString(word) > n {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			r := []rune(word)
			lines = append(lines, string(r[:n]))
			word = string(r[n:])
		}
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= n:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
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
