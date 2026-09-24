package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// gProg è il progress attivo: i log vi passano per non rovinare le barre.
var gProg *Progress

// notef stampa una nota fuori banda: sopra le barre se il progress è attivo,
// altrimenti su stderr.
func notef(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if gProg != nil {
		gProg.Log(s)
		return
	}
	fmt.Fprintln(os.Stderr, s)
}

// fileProg è lo stato di un download in corso.
type fileProg struct {
	name string
	size int64 // -1 se ignota
	done int64 // atomico

	t0        time.Time
	lastBytes int64
	lastTime  time.Time
	speed     float64
}

// Progress disegna una barra semplice per ogni file in download.
type Progress struct {
	mu        sync.Mutex
	files     []*fileProg
	tty       bool
	color     bool
	quiet     bool
	liveLines int
	start     time.Time

	doneCnt  int64
	failCnt  int64
	totBytes int64

	stop chan struct{}
	once sync.Once
}

func NewProgress(quiet bool) *Progress {
	tty := isTTY(os.Stderr)
	return &Progress{
		tty:   tty && !quiet,
		color: tty && !quiet && os.Getenv("NO_COLOR") == "",
		quiet: quiet,
		start: time.Now(),
		stop:  make(chan struct{}),
	}
}

func (p *Progress) run() {
	if !p.tty {
		return
	}
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			p.render()
			p.mu.Unlock()
		}
	}
}

func (p *Progress) startFile(name string, size int64) *fileProg {
	fp := &fileProg{name: filepath.Base(name), size: size, t0: time.Now(), lastTime: time.Now()}
	p.mu.Lock()
	p.files = append(p.files, fp)
	p.mu.Unlock()
	return fp
}

func (p *Progress) finishFile(fp *fileProg, ok bool) {
	p.mu.Lock()
	for i, f := range p.files {
		if f == fp {
			p.files = append(p.files[:i], p.files[i+1:]...)
			break
		}
	}
	if ok {
		atomic.AddInt64(&p.doneCnt, 1)
		atomic.AddInt64(&p.totBytes, atomic.LoadInt64(&fp.done))
		p.logLocked(p.doneLine(fp))
	} else {
		// Il dettaglio dell'errore lo stampa il chiamante; qui aggiorno solo i
		// contatori e ridisegno.
		atomic.AddInt64(&p.failCnt, 1)
		p.render()
	}
	p.mu.Unlock()
}

// cancelFile rimuove la barra di un file che verrà ritentato con un link fresco,
// senza toccare i contatori (non è né un successo né un fallimento definitivo).
func (p *Progress) cancelFile(fp *fileProg) {
	p.mu.Lock()
	for i, f := range p.files {
		if f == fp {
			p.files = append(p.files[:i], p.files[i+1:]...)
			break
		}
	}
	p.render()
	p.mu.Unlock()
}

func (p *Progress) doneLine(fp *fileProg) string {
	d := atomic.LoadInt64(&fp.done)
	return fmt.Sprintf(" %s %s  %s", col(p.color, aGreen, "✓"), fp.name, col(p.color, aDim, humanShort(d)))
}

// Log stampa un messaggio sopra l'area viva.
func (p *Progress) Log(s string) {
	p.mu.Lock()
	p.logLocked(s)
	p.mu.Unlock()
}

func (p *Progress) logLocked(s string) {
	if p.quiet {
		return
	}
	if !p.tty {
		fmt.Fprintln(os.Stderr, s)
		return
	}
	var b strings.Builder
	if p.liveLines > 1 {
		fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
	}
	b.WriteString("\r\033[J")
	b.WriteString(s)
	b.WriteString("\n")
	p.liveLines = 0
	fmt.Fprint(os.Stderr, b.String())
	p.render()
}

func (p *Progress) render() {
	if !p.tty {
		return
	}
	lines := p.buildLines()
	var b strings.Builder
	if p.liveLines > 1 {
		fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
	}
	b.WriteString("\r\033[J")
	for i, ln := range lines {
		b.WriteString(ln)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	p.liveLines = len(lines)
	fmt.Fprint(os.Stderr, b.String())
}

func (p *Progress) buildLines() []string {
	now := time.Now()
	w := termWidthOr(80)
	var lines []string
	for _, fp := range p.files {
		lines = append(lines, p.fileLine(fp, w, now))
	}
	if len(p.files) == 0 {
		return lines
	}
	done := atomic.LoadInt64(&p.doneCnt)
	fail := atomic.LoadInt64(&p.failCnt)
	foot := fmt.Sprintf(" %s in corso · %s completati", col(p.color, aBold, itoa(len(p.files))), col(p.color, aBold, itoa(int(done))))
	if fail > 0 {
		foot += " · " + col(p.color, aRed, itoa(int(fail))+" falliti")
	}
	lines = append(lines, foot)
	return lines
}

func (p *Progress) fileLine(fp *fileProg, w int, now time.Time) string {
	d := atomic.LoadInt64(&fp.done)
	if ddt := now.Sub(fp.lastTime).Seconds(); ddt >= 0.1 {
		inst := float64(d-fp.lastBytes) / ddt
		if fp.speed == 0 {
			fp.speed = inst
		} else {
			fp.speed = 0.6*fp.speed + 0.4*inst
		}
		fp.lastBytes, fp.lastTime = d, now
	}

	nameW := 28
	name := fitName(fp.name, nameW)
	barW := w - nameW - 46
	if barW < 8 {
		barW = 8
	}
	if barW > 28 {
		barW = 28
	}

	var frac float64
	if fp.size > 0 {
		frac = float64(d) / float64(fp.size)
	}
	bar := mainBar(frac, barW, p.color)

	var sizes, pct, eta string
	if fp.size > 0 {
		pct = fmt.Sprintf("%3.0f%%", frac*100)
		sizes = humanShort(d) + "/" + humanShort(fp.size)
		if fp.speed > 0 {
			eta = etaStr(float64(fp.size-d) / fp.speed)
		} else {
			eta = "--"
		}
	} else {
		pct = "  · "
		sizes = humanShort(d)
	}
	spd := humanShort(int64(fp.speed)) + "/s"

	return fmt.Sprintf(" %s %s  %s %s  %s  %s  %s",
		col(p.color, aCyan, "⬇"), name, bar, pct,
		col(p.color, aBold, sizes),
		col(p.color, aGreen, spd),
		col(p.color, aDim, eta))
}

func (p *Progress) finish() {
	p.once.Do(func() { close(p.stop) })
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty && p.liveLines > 0 {
		var b strings.Builder
		if p.liveLines > 1 {
			fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
		}
		b.WriteString("\r\033[J")
		fmt.Fprint(os.Stderr, b.String())
		p.liveLines = 0
	}
	b := atomic.LoadInt64(&p.totBytes)
	f := atomic.LoadInt64(&p.doneCnt)
	el := time.Since(p.start).Seconds()
	avg := float64(0)
	if el > 0 {
		avg = float64(b) / el
	}
	fmt.Fprintf(os.Stderr, "%s %s file · %s in %s · media %s\n",
		col(p.color, aBold+aGreen, "✓"), itoa(int(f)), humanShort(b), etaStr(el), humanShort(int64(avg))+"/s")
}

// ---- barre e formattazione ----

var barEighths = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

func mainBar(frac float64, width int, color bool) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full := frac * float64(width)
	fullCells := int(full)
	var filled strings.Builder
	for i := 0; i < fullCells && i < width; i++ {
		filled.WriteRune('█')
	}
	rem := width - fullCells
	var partial string
	if fullCells < width {
		idx := int((full - float64(fullCells)) * 8)
		if idx > 0 {
			partial = string(barEighths[idx])
			rem--
		}
	}
	empty := strings.Repeat("░", max(rem, 0))
	return col(color, aDim, "▕") + col(color, aGreen, filled.String()+partial) + col(color, aDim, empty) + col(color, aDim, "▏")
}

func humanShort(n int64) string {
	const u = 1024.0
	f := float64(n)
	if n < 1024 {
		return itoa(int(n)) + "B"
	}
	units := "KMGTPE"
	i := -1
	for f >= u && i < len(units)-1 {
		f /= u
		i++
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f%c", f, units[i])
	}
	return fmt.Sprintf("%.1f%c", f, units[i])
}

func etaStr(sec float64) string {
	if sec < 0 || sec > 99*3600 {
		return "--"
	}
	s := int(sec + 0.5)
	h, m, ss := s/3600, (s%3600)/60, s%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, ss)
	default:
		return fmt.Sprintf("%ds", ss)
	}
}

func fitName(s string, n int) string {
	r := []rune(s)
	if len(r) == n {
		return s
	}
	if len(r) > n {
		if n <= 1 {
			return string(r[:n])
		}
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

func termWidthOr(def int) int {
	w, _ := termSize()
	if w <= 0 {
		return def
	}
	return w
}
