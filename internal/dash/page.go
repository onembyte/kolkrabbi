// Package dash renders Kolkrabbi's local usage dashboard.
//
// Everything here is drawn on the server. There is no chart library, no asset
// pipeline and no scripting requirement: the page is the same in a browser, a
// screenshot and a text-mode reader, and it carries no third-party code.
package dash

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/stats"
)

// Page renders the whole dashboard from records already loaded, reporting how
// many log lines could not be read so a total is never presented as complete
// when it is not.
func Page(records []stats.Record, skipped int, cards []SessionCard, shared []SharedCheckout) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>kolk dash</title>`)
	b.WriteString("<style>" + pageCSS + "</style></head><body>")
	b.WriteString(`<h1>kolk dash</h1>`)
	b.WriteString(`<p class="sub">Everything below is computed on this machine from your own usage log. Nothing is sent anywhere.</p>`)

	if skipped > 0 {
		fmt.Fprintf(&b, `<p class="warn">%d unreadable line(s) in the usage log were skipped, so these totals are incomplete.</p>`, skipped)
	}

	// Sessions first, and blocked sessions first within that. The usage
	// history is what happened; a blocked session is what is happening, and it
	// is the only thing on this page that is waiting for the person reading it.
	b.WriteString(renderSharedCheckouts(shared))
	b.WriteString(renderSessionCards(cards))

	rows := stats.Aggregate(records)
	if len(rows) == 0 {
		if len(cards) == 0 {
			b.WriteString(emptyState)
		}
		b.WriteString("</body></html>")
		return b.String()
	}

	writeLeaderboard(&b, rows)
	writeSpendChart(&b, records)
	writeBreakdown(&b, records)
	writeSessions(&b, records)

	b.WriteString("</body></html>")
	return b.String()
}

const emptyState = `<section><h2>No usage recorded yet</h2>
<p>Run <code>kolk</code> and ask it something. Every model call is appended to your local usage log,
and this page will then show which models you use, what they cost, and how they rate.</p>
<p>Rate a turn with <code>/rate 1-5</code> to see quality beside cost.</p></section>`

func writeLeaderboard(b *strings.Builder, rows []stats.ModelRow) {
	total := 0.0
	for _, row := range rows {
		total += row.Cost
	}
	b.WriteString(`<section><h2>Which model earns its cost</h2>`)
	fmt.Fprintf(b, `<p class="sub">%s across %d models.</p>`, money(total), len(rows))
	b.WriteString(`<table><thead><tr><th>Model</th><th class="n">Calls</th><th class="n">Tokens</th>` +
		`<th class="n">Cost</th><th class="n">Avg</th><th class="n">Rating</th></tr></thead><tbody>`)
	for _, row := range rows {
		rating := "—"
		if row.Ratings > 0 {
			// The count is shown beside the score: a 5.0 from one rated turn is
			// not a ranking, and hiding the sample size invites reading it as one.
			rating = fmt.Sprintf("%.1f★ <span class=\"dim\">(%d)</span>", row.AvgRating, row.Ratings)
		}
		fmt.Fprintf(b, `<tr><td>%s</td><td class="n">%d</td><td class="n">%s</td>`+
			`<td class="n">%s</td><td class="n">%dms</td><td class="n">%s</td></tr>`,
			html.EscapeString(row.Model), row.Calls, count(row.Tokens), money(row.Cost), row.AvgMs, rating)
	}
	b.WriteString(`</tbody></table></section>`)
}

// dailySpend buckets cost by calendar day in the machine's own timezone, which
// is the one the user thinks in.
func dailySpend(records []stats.Record) ([]string, []float64) {
	byDay := map[string]float64{}
	for _, record := range records {
		if record.Kind != "call" || record.Time.IsZero() {
			continue
		}
		byDay[record.Time.Local().Format("2006-01-02")] += record.Cost
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)
	values := make([]float64, 0, len(days))
	for _, day := range days {
		values = append(values, byDay[day])
	}
	return days, values
}

func writeSpendChart(b *strings.Builder, records []stats.Record) {
	days, values := dailySpend(records)
	if len(days) == 0 {
		return
	}
	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	const width, height, pad = 720.0, 180.0, 24.0
	barWidth := (width - pad*2) / float64(len(days))

	b.WriteString(`<section><h2>What it cost, by day</h2>`)
	fmt.Fprintf(b, `<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="Daily spend">`, width, height)
	fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="axis"/>`,
		pad, height-pad, width-pad, height-pad)
	for i, value := range values {
		barHeight := 0.0
		if peak > 0 {
			barHeight = (height - pad*2) * (value / peak)
		}
		x := pad + float64(i)*barWidth
		fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" class="bar"><title>%s — %s</title></rect>`,
			x+1, height-pad-barHeight, barWidth-2, barHeight, html.EscapeString(days[i]), money(value))
	}
	b.WriteString(`</svg>`)
	fmt.Fprintf(b, `<p class="sub">%s to %s · peak %s in a day</p></section>`,
		html.EscapeString(days[0]), html.EscapeString(days[len(days)-1]), money(peak))
}

func writeBreakdown(b *strings.Builder, records []stats.Record) {
	byEffort, byMode := map[string]float64{}, map[string]float64{}
	for _, record := range records {
		if record.Kind != "call" {
			continue
		}
		if record.Effort != "" {
			// quick/standard/deep/ultra and low/medium/high/max are the same
			// four levels. Showing both spellings splits one level's spend
			// across two rows that look like two levels.
			effort := record.Effort
			if canonical, ok := engine.NormalizeEffort(effort); ok {
				effort = canonical
			}
			byEffort[effort] += record.Cost
		}
		if record.Mode != "" {
			byMode[record.Mode] += record.Cost
		}
	}
	if len(byEffort) == 0 && len(byMode) == 0 {
		return
	}
	b.WriteString(`<section><h2>Where the effort went</h2><div class="cols">`)
	writeCostList(b, "By effort", byEffort)
	writeCostList(b, "By mode", byMode)
	b.WriteString(`</div></section>`)
}

func writeCostList(b *strings.Builder, title string, costs map[string]float64) {
	if len(costs) == 0 {
		return
	}
	keys := make([]string, 0, len(costs))
	for key := range costs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return costs[keys[i]] > costs[keys[j]] })
	fmt.Fprintf(b, `<div><h3>%s</h3><table><tbody>`, html.EscapeString(title))
	for _, key := range keys {
		fmt.Fprintf(b, `<tr><td>%s</td><td class="n">%s</td></tr>`,
			html.EscapeString(key), money(costs[key]))
	}
	b.WriteString(`</tbody></table></div>`)
}

func money(v float64) string {
	if v == 0 {
		return "$0"
	}
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func count(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

const pageCSS = `
.cards{display:flex;flex-wrap:wrap;gap:.75rem}
.card{border:1px solid #d8d8d8;border-radius:6px;padding:.6rem .8rem;min-width:14rem}
.card.blocked{border-color:#b45309;border-width:2px;background:#fffbeb}
.card h3{margin:0 0 .25rem;font-size:1rem}
.card .state{margin:0 0 .2rem;font-size:.85rem}
.card .meta{margin:0;font-size:.8rem;color:#555}

:root{color-scheme:light dark}
body{font:15px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;margin:2rem auto;max-width:56rem;padding:0 1rem}
h1{font-size:1.4rem;margin:0}
h2{font-size:1.05rem;margin:2rem 0 .25rem}
h3{font-size:.95rem;margin:1rem 0 .25rem}
.sub{opacity:.7;margin:.25rem 0 1rem}
.dim{opacity:.55}
.warn{border-left:3px solid #b26b00;padding:.5rem .75rem;background:rgba(178,107,0,.08)}
table{border-collapse:collapse;width:100%}
th,td{text-align:left;padding:.3rem .6rem;border-bottom:1px solid rgba(128,128,128,.25)}
th.n,td.n{text-align:right}
svg{width:100%;height:auto}
.axis{stroke:rgba(128,128,128,.5);stroke-width:1}
.bar{fill:#7c5cff}
.cols{display:flex;gap:2rem;flex-wrap:wrap}
.cols>div{flex:1 1 16rem}
code{background:rgba(128,128,128,.15);padding:.1rem .3rem;border-radius:3px}
`

// sessionRow is one session's totals.
type sessionRow struct {
	ID     string
	Calls  int
	Tokens int
	Cost   float64
	Last   time.Time
	Models map[string]bool
}

// writeSessions lists the most recent sessions and what each one cost.
//
// Records written before sessions were tagged carry no id, and a row nobody can
// identify is a row nobody can act on, so they are left out entirely rather than
// shown blank.
func writeSessions(b *strings.Builder, records []stats.Record) {
	bySession := map[string]*sessionRow{}
	for _, record := range records {
		if record.Kind != "call" || record.Session == "" {
			continue
		}
		row, ok := bySession[record.Session]
		if !ok {
			row = &sessionRow{ID: record.Session, Models: map[string]bool{}}
			bySession[record.Session] = row
		}
		row.Calls++
		row.Tokens += record.PromptTokens + record.CompletionTokens
		row.Cost += record.Cost
		row.Models[record.Model] = true
		if record.Time.After(row.Last) {
			row.Last = record.Time
		}
	}
	if len(bySession) == 0 {
		return
	}
	rows := make([]*sessionRow, 0, len(bySession))
	for _, row := range bySession {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Last.After(rows[j].Last) })
	if len(rows) > maxSessionRows {
		rows = rows[:maxSessionRows]
	}

	b.WriteString(`<section><h2>Recent sessions</h2>`)
	b.WriteString(`<table><thead><tr><th>Session</th><th>Last used</th><th class="n">Calls</th>` +
		`<th class="n">Tokens</th><th class="n">Cost</th><th>Models</th></tr></thead><tbody>`)
	for _, row := range rows {
		models := make([]string, 0, len(row.Models))
		for model := range row.Models {
			models = append(models, model)
		}
		sort.Strings(models)
		fmt.Fprintf(b, `<tr><td>%s</td><td>%s</td><td class="n">%d</td><td class="n">%s</td>`+
			`<td class="n">%s</td><td>%s</td></tr>`,
			html.EscapeString(row.ID), row.Last.Local().Format("2006-01-02 15:04"),
			row.Calls, count(row.Tokens), money(row.Cost),
			html.EscapeString(strings.Join(models, ", ")))
	}
	b.WriteString(`</tbody></table>`)
	b.WriteString(`<p class="sub">Resume one with <code>kolk -s &lt;id&gt;</code>, or read it with <code>kolk sessions export &lt;id&gt;</code>.</p></section>`)
}

// maxSessionRows keeps the page readable. The full list is what
// `kolk sessions` is for.
const maxSessionRows = 20
