package app

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Progress interface {
	Start(step string)
	Detail(key string, value string)
	Info(key string, value string)
	Success(message string)
	Warn(message string)
	Fail(message string)
}

type cliProgress struct {
	writer  io.Writer
	quiet   bool
	verbose bool
	tty     bool
	color   bool

	mu      sync.Mutex
	writeMu sync.Mutex
	step    string
	stop    chan struct{}
	done    chan struct{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newProgress(writer io.Writer, quiet, verbose bool) Progress {
	tty := isTerminal(writer)
	return &cliProgress{
		writer:  writer,
		quiet:   quiet,
		verbose: verbose,
		tty:     tty,
		color:   tty && os.Getenv("NO_COLOR") == "",
	}
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p *cliProgress) Start(step string) {
	p.stopSpinner()
	p.mu.Lock()
	p.step = step
	if p.quiet {
		p.mu.Unlock()
		return
	}
	if p.tty {
		stop := make(chan struct{})
		done := make(chan struct{})
		p.stop = stop
		p.done = done
		p.mu.Unlock()
		go p.spin(step, stop, done)
		return
	}
	p.mu.Unlock()
	p.writeLine("→ %s", step)
}

func (p *cliProgress) Detail(key string, value string) {
	if p.quiet || !p.verbose {
		return
	}
	p.writeStatus("ℹ", key, value, colorCyan)
}

func (p *cliProgress) Info(key string, value string) {
	p.stopSpinner()
	p.writeStatus("ℹ", key, value, colorCyan)
}

func (p *cliProgress) Success(message string) {
	step := p.currentStep()
	p.stopSpinner()
	if p.quiet {
		return
	}
	p.writeStatus("✓", step, message, colorGreen)
}

func (p *cliProgress) Warn(message string) {
	p.stopSpinner()
	p.writeSymbolLine("!", message, colorYellow)
}

func (p *cliProgress) Fail(message string) {
	step := p.currentStep()
	p.stopSpinner()
	p.writeStatus("✗", step, message, colorRed)
}

func (p *cliProgress) currentStep() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.step
}

func (p *cliProgress) spin(step string, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	for {
		p.writeTTY("%s %s", p.colorize(spinnerFrames[frame], colorCyan), step)
		frame = (frame + 1) % len(spinnerFrames)
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

func (p *cliProgress) stopSpinner() {
	p.mu.Lock()
	stop := p.stop
	done := p.done
	p.stop = nil
	p.done = nil
	p.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

func (p *cliProgress) writeStatus(symbol, step, message, colorCode string) {
	if step == "" {
		p.writeSymbolLine(symbol, message, colorCode)
		return
	}
	if message == "" {
		p.writeLine("%s %s", p.colorize(symbol, colorCode), step)
		return
	}
	p.writeLine("%s %s  %s", p.colorize(symbol, colorCode), step, message)
}

func (p *cliProgress) writeSymbolLine(symbol, message, colorCode string) {
	if message == "" {
		p.writeLine("%s", p.colorize(symbol, colorCode))
		return
	}
	p.writeLine("%s %s", p.colorize(symbol, colorCode), message)
}

func (p *cliProgress) writeLine(format string, args ...any) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if p.tty {
		fmt.Fprint(p.writer, "\r\033[2K")
	}
	fmt.Fprintf(p.writer, format+"\n", args...)
}

func (p *cliProgress) writeTTY(format string, args ...any) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	fmt.Fprint(p.writer, "\r\033[2K")
	fmt.Fprintf(p.writer, format, args...)
}

func (p *cliProgress) colorize(text string, colorCode string) string {
	if !p.color {
		return text
	}
	return colorCode + text + "\033[0m"
}

const (
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorMagenta = "\033[35m"
	colorYellow  = "\033[33m"
	colorRed     = "\033[31m"
	colorDim     = "\033[2m"
	colorReset   = "\033[0m"
)
