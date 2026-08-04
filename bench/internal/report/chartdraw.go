package report

import (
	"fmt"
	"math"
	"strings"
)

// The drawing half of the chart: scales, axes, rows and marks.
//
// Every element used here is one an SVG referenced as an image renders without
// question - svg, rect, circle, line, text, g - and every attribute is an
// inline presentation attribute. There is no style element, no script, no
// external reference and no font file. The chart is referenced from markdown
// as an image rather than inlined, so the sanitiser that narrows inline SVG in
// markdown is not the rule that applies; what applies is that an image-embedded
// SVG cannot script and cannot fetch, and this one does neither.

// fontStack is a generic family list with no assumption that any particular
// face is installed, and every text element carries an explicit font-size.
const fontStack = "system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"

// scale maps a measurement to an x position inside one panel.
type scale struct {
	left, width float64
	lo, hi      float64
	log         bool
}

func (s scale) x(v float64) float64 {
	if s.log {
		if v <= 0 {
			return s.left
		}

		return s.left + s.width*(math.Log10(v)-math.Log10(s.lo))/(math.Log10(s.hi)-math.Log10(s.lo))
	}

	return s.left + s.width*(v-s.lo)/(s.hi-s.lo)
}

// tick is one gridline.
type tick struct {
	at    float64
	label string
}

// newLogScale spans the decades the measurements actually occupy, rounded out
// to whole powers of ten so that every gridline is a round number.
func newLogScale(groups []chartGroup, left, width float64) scale {
	lo, hi := math.Inf(1), 0.0

	forEachRow(groups, func(r chartRow) {
		if !r.HasDuration {
			return
		}

		lo = math.Min(lo, math.Min(r.ColdSec, r.WarmSec))
		hi = math.Max(hi, math.Max(r.ColdSec, r.WarmSec))
	})

	if math.IsInf(lo, 1) || hi == 0 {
		lo, hi = 1e-9, 1e-3
	}

	return scale{
		left: left, width: width, log: true,
		lo: math.Pow(10, math.Floor(math.Log10(lo))),
		hi: math.Pow(10, math.Ceil(math.Log10(hi))),
	}
}

// newLinScale starts at zero, always. A bar chart with a cut-off baseline is
// the commonest way a benchmark chart misleads, so the baseline is not an
// option this code offers.
func newLinScale(groups []chartGroup, left, width float64) scale {
	hi := 0.0

	forEachRow(groups, func(r chartRow) {
		if r.HasAllocs {
			hi = math.Max(hi, math.Max(r.ColdAlloc, r.WarmAlloc))
		}
	})

	return scale{left: left, width: width, lo: 0, hi: niceCeil(hi)}
}

// niceCeil rounds up to 1, 2 or 5 times a power of ten, so the axis ends on a
// number a reader can divide by.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}

	mag := math.Pow(10, math.Floor(math.Log10(v)))

	for _, m := range []float64{1, 1.5, 2, 2.5, 3, 4, 5, 10} {
		if v <= m*mag {
			return m * mag
		}
	}

	return 10 * mag
}

func (s scale) ticks() []tick {
	if s.log {
		return s.logTicks()
	}

	return s.linTicks()
}

func (s scale) logTicks() []tick {
	var out []tick

	for e := math.Log10(s.lo); e <= math.Log10(s.hi)+0.5; e++ {
		v := math.Pow(10, e)
		out = append(out, tick{at: v, label: formatDuration(v)})
	}

	return out
}

func (s scale) linTicks() []tick {
	out := make([]tick, 0, 5)

	for i := range 5 {
		v := s.hi * float64(i) / 4

		out = append(out, tick{at: v, label: trimFloat(v)})
	}

	return out
}

func trimFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}

func forEachRow(groups []chartGroup, fn func(chartRow)) {
	for _, g := range groups {
		for _, r := range g.Rows {
			fn(r)
		}
	}
}

func writeChartHeader(b *strings.Builder, in *Input, th Theme) {
	text(b, sideMargin, 26, 15, "600", th.Text, "start",
		"ferry against the libraries people choose between")
	text(b, sideMargin, 45, 11, "400", th.Muted, "start",
		"One populated struct, one source, asserted identical across every library before anything was timed.")
	text(b, sideMargin, 60, 11, "400", th.Muted, "start",
		subtitle(in))
	text(b, sideMargin, 75, 11, "400", th.Muted, "start",
		"Rows are ordered by warm time, fastest first, in every scenario. "+
			"A library with no mark was not measured and says so.")

	writeLegend(b, th, 88)
}

func subtitle(in *Input) string {
	return fmt.Sprintf("%s, Go %s, -count %s, -benchtime %s. Whiskers are benchstat's %s confidence interval.",
		value(in.Meta.Runner), value(in.Meta.GoVersion),
		value(in.Meta.Count), value(in.Meta.Benchtime), "95%")
}

func writeLegend(b *strings.Builder, th Theme, y float64) {
	x := sideMargin

	circle(b, x+4, y-4, markRadius, th.Bg, th.Cold, 1.6)
	text(b, x+14, y, 11, "400", th.Muted, "start", "cold: constructed every iteration")

	x += 214
	circle(b, x+4, y-4, markRadius, th.Warm, th.Warm, 0)
	text(b, x+14, y, 11, "400", th.Muted, "start", "warm: constructed once, then run")

	x += 210
	circle(b, x+4, y-4, 2.4, "none", th.Axis, 1.2)
	text(b, x+14, y, 11, "400", th.Muted, "start", "marks ferry's row, which is privileged nowhere else")
}

func writeChartFooter(b *strings.Builder, th Theme, height float64) {
	y := height - 26
	text(b, sideMargin, y, 10.5, "400", th.Muted, "start",
		"The time panel is a log scale, so a mark twice as far right is ten times the number, not twice it. "+
			"Marks carry no bar because a bar on a log axis reads as a proportion it does not have.")
	text(b, sideMargin, y+14, 10.5, "400", th.Muted, "start",
		"The allocation panel is linear and starts at zero. "+caveatDagger+
			" marks a warm mark that is not comparable with the others in its scenario, because that "+
			"library reads and parses the file once at construction; its cold mark is the comparable one.")
	text(b, sideMargin, y+28, 10.5, "400", th.Muted, "start",
		"Generated by bench/cmd/perfreport from benchstat output; no figure on this chart was typed.")
}

// writeAxis draws one panel's title, gridlines and tick labels.
//
// Both scales place their ticks at even intervals across the panel and that is
// not a shortcut: the log scale's ticks are whole decades, which are equally
// spaced on a log axis by definition, and the linear scale's are four equal
// steps from zero. Either way the position a tick is drawn at is the position
// scale.x would give it.
func writeAxis(b *strings.Builder, th Theme, s scale, height float64, title string) {
	top := headerHeight - 6
	bottom := height - footerHeight - 6
	ticks := s.ticks()

	text(b, s.left, headerHeight-16, 11.5, "600", th.Text, "start", title)

	for _, t := range ticks {
		x := s.x(t.at)

		line(b, x, top, x, bottom, th.Grid, 1)
		text(b, x, bottom+14, 10, "400", th.Axis, "middle", t.label)
	}
}

func writeGroups(b *strings.Builder, groups []chartGroup, th Theme, sec, alloc scale) {
	y := headerHeight + 10

	for _, g := range groups {
		text(b, sideMargin, y+10, 12, "600", th.Text, "start", g.Scenario)

		y += groupHead

		for _, r := range g.Rows {
			writeRow(b, r, th, sec, alloc, y)

			y += rowHeight
		}

		y += groupGap
	}
}

func writeRow(b *strings.Builder, r chartRow, th Theme, sec, alloc scale, y float64) {
	mid := y + rowHeight/2

	if r.IsFerry {
		circle(b, sideMargin+4, mid, 2.4, "none", th.Axis, 1.2)
	}

	label := r.Impl
	if r.Caveat {
		label += " " + caveatDagger
	}

	text(b, sideMargin+13, mid+3.5, 11, "400", th.Text, "start", label)

	if !r.Measured {
		text(b, sec.left, mid+3.5, 10.5, "400", th.Absent, "start", notMeasured)
		text(b, alloc.left, mid+3.5, 10.5, "400", th.Absent, "start", notMeasured)

		return
	}

	writeDurationMarks(b, r, th, sec, mid)
	writeAllocBars(b, r, th, alloc, y)
}

func writeDurationMarks(b *strings.Builder, r chartRow, th Theme, sec scale, mid float64) {
	cx, wx := sec.x(r.ColdSec), sec.x(r.WarmSec)

	// The connector says how far the cold mark is from the warm one, which is
	// the whole cold/warm finding for a library that caches anything.
	line(b, math.Min(cx, wx), mid, math.Max(cx, wx), mid, th.Axis, 1)

	writeWhisker(b, sec, r.ColdSec, r.ColdSecCI, r.HasColdCI, th.Cold, mid)
	writeWhisker(b, sec, r.WarmSec, r.WarmSecCI, r.HasWarmCI, th.Warm, mid)

	circle(b, cx, mid, markRadius, th.Bg, th.Cold, 1.6)
	circle(b, wx, mid, markRadius, th.Warm, th.Warm, 0)
}

func writeWhisker(b *strings.Builder, s scale, v, ci float64, has bool, colour string, mid float64) {
	if !has || ci <= 0 {
		return
	}

	lo, hi := s.x(v*(1-ci)), s.x(v*(1+ci))

	line(b, lo, mid, hi, mid, colour, 1.2)
	line(b, lo, mid-capHalf, lo, mid+capHalf, colour, 1.2)
	line(b, hi, mid-capHalf, hi, mid+capHalf, colour, 1.2)
}

func writeAllocBars(b *strings.Builder, r chartRow, th Theme, s scale, y float64) {
	if !r.HasAllocs {
		text(b, s.left, y+rowHeight/2+3.5, 10.5, "400", th.Absent, "start", notMeasured)

		return
	}

	const h = 5.0

	rect(b, s.left, y+3, math.Max(s.x(r.ColdAlloc)-s.left, 0.6), h, th.BarCold)
	rect(b, s.left, y+3+h+1, math.Max(s.x(r.WarmAlloc)-s.left, 0.6), h, th.BarWarm)
}

// The four primitives, so that no drawing code writes raw markup and every
// element in the output is one of exactly four kinds.

func text(b *strings.Builder, x, y, size float64, weight, fill, anchor, s string) {
	fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-family="%s" font-size="%.1f" font-weight="%s" `+
		`fill="%s" text-anchor="%s">%s</text>`+"\n",
		x, y, fontStack, size, weight, fill, anchor, escapeXML(s))
}

func line(b *strings.Builder, x1, y1, x2, y2 float64, stroke string, w float64) {
	fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`+"\n",
		x1, y1, x2, y2, stroke, w)
}

func rect(b *strings.Builder, x, y, w, h float64, fill string) {
	fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n", x, y, w, h, fill)
}

func circle(b *strings.Builder, cx, cy, r float64, fill, stroke string, w float64) {
	if w == 0 {
		fmt.Fprintf(b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s"/>`+"\n", cx, cy, r, fill)

		return
	}

	fmt.Fprintf(b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`+"\n",
		cx, cy, r, fill, stroke, w)
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
