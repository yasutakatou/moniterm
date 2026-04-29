package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nsf/termbox-go"
)

type App struct {
	upperContent string
	inputBuffer  string
	cursorIdx    int
	history      []string
	cmdHistory   []string
	historyIdx   int
	cwd          string
	ps1          string
	showPopup    bool // 【追加】ポップアップ表示フラグ
	mutex        sync.Mutex
}

type monitorCommand struct {
	LABEL   string
	COMMAND string
}

var (
	monitorCommands []monitorCommand
	shell           string
)

func main() {
	_interval := flag.Int("interval", 3, "[-int=Command check interval]")
	_config := flag.String("config", "moniterm.ini", "[-config=Config filename]")
	_Shell := flag.String("shell", "/bin/bash", "[-shell=Specifies the shell to use in the case of linux]")

	flag.Parse()
	shell = string(*_Shell)

	if loadConfig(*_config) == false {
		log.Fatalf("Fail to read config file")
		os.Exit(1)
	}

	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()

	u, _ := user.Current()
	h, _ := os.Hostname()
	cwd, _ := os.Getwd()

	app := &App{
		upperContent: "",
		history:      []string{""},
		cmdHistory:   []string{},
		historyIdx:   -1,
		cursorIdx:    0,
		cwd:          cwd,
		ps1:          fmt.Sprintf("%s@%s", u.Username, h),
		showPopup:    false, // 初期値は非表示
	}

	app.draw()

	go func() {
		ticker := time.NewTicker(time.Duration(int(*_interval)) * time.Second)
		app.runPeriodicCommand()
		for range ticker.C {
			app.runPeriodicCommand()
		}
	}()

	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			// 【追加】Ctrl+Pでポップアップをトグル
			if ev.Key == termbox.KeyCtrlP {
				app.mutex.Lock()
				app.showPopup = !app.showPopup
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyCtrlC || ev.Key == termbox.KeyEsc {
				return
			} else if !app.showPopup { // ポップアップが出ていない時だけ入力を受け付ける
				if ev.Key == termbox.KeyEnter {
					app.handleCommand()
				} else if ev.Key == termbox.KeyTab {
					app.handleTab()
				} else if ev.Key == termbox.KeyArrowUp {
					app.navigateHistory(-1)
				} else if ev.Key == termbox.KeyArrowDown {
					app.navigateHistory(1)
				} else if ev.Key == termbox.KeyArrowLeft {
					app.mutex.Lock()
					if app.cursorIdx > 0 { app.cursorIdx-- }
					app.mutex.Unlock()
				} else if ev.Key == termbox.KeyArrowRight {
					app.mutex.Lock()
					r := []rune(app.inputBuffer)
					if app.cursorIdx < len(r) { app.cursorIdx++ }
					app.mutex.Unlock()
				} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
					app.mutex.Lock()
					r := []rune(app.inputBuffer)
					if app.cursorIdx > 0 {
						newRunes := append(r[:app.cursorIdx-1], r[app.cursorIdx:]...)
						app.inputBuffer = string(newRunes)
						app.cursorIdx--
						app.historyIdx = -1
					}
					app.mutex.Unlock()
				} else if ev.Key == termbox.KeyDelete {
					app.mutex.Lock()
					r := []rune(app.inputBuffer)
					if app.cursorIdx < len(r) {
						newRunes := append(r[:app.cursorIdx], r[app.cursorIdx+1:]...)
						app.inputBuffer = string(newRunes)
						app.historyIdx = -1
					}
					app.mutex.Unlock()
				} else if ev.Key == termbox.KeySpace {
					app.insertChar(' ')
				} else if ev.Ch != 0 {
					app.insertChar(ev.Ch)
				}
			}
		case termbox.EventResize:
		case termbox.EventError:
			panic(ev.Err)
		}
		app.draw()
	}
}

// 【追加】ポップアップ描画処理
func (a *App) drawPopup(w, h int) {
	// ポップアップのサイズ計算
	pW := w * 4 / 5
	pH := h * 4 / 5
	pX := (w - pW) / 2
	pY := (h - pH) / 2

	// 背景と枠線の描画
	for y := pY; y < pY+pH; y++ {
		for x := pX; x < pX+pW; x++ {
			char := ' '
			fg := termbox.ColorDefault
			bg := termbox.ColorBlack // 背景を黒に

			if y == pY || y == pY+pH-1 {
				char = '-'
			} else if x == pX || x == pX+pW-1 {
				char = '|'
			}
			termbox.SetCell(x, y, char, fg, bg)
		}
	}

	// ヘッダー
	title := " Monitor Commands Configuration (Ctrl+P to close) "
	printString(pX+(pW-len(title))/2, pY, title, termbox.ColorYellow|termbox.AttrBold, termbox.ColorBlack)

	// 表の項目描画
	headerLabel := "LABEL"
	headerCmd   := "COMMAND"
	col1Width := 15
	printString(pX+2, pY+2, headerLabel, termbox.ColorCyan, termbox.ColorBlack)
	printString(pX+2+col1Width, pY+2, headerCmd, termbox.ColorCyan, termbox.ColorBlack)
	
	line := strings.Repeat("-", pW-4)
	printString(pX+2, pY+3, line, termbox.ColorWhite, termbox.ColorBlack)

	// リストの内容
	for i, cmd := range monitorCommands {
		if i >= pH-6 { break } // 画面内に収まる分だけ
		rowY := pY + 4 + i
		printString(pX+2, rowY, truncate(cmd.LABEL, col1Width-2), termbox.ColorWhite, termbox.ColorBlack)
		printString(pX+2+col1Width, rowY, truncate(cmd.COMMAND, pW-col1Width-5), termbox.ColorWhite, termbox.ColorBlack)
	}
}

func (a *App) insertChar(ch rune) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	r := []rune(a.inputBuffer)
	newRunes := append(r[:a.cursorIdx], append([]rune{ch}, r[a.cursorIdx:]...)...)
	a.inputBuffer = string(newRunes)
	a.cursorIdx++
	a.historyIdx = -1
}

func (a *App) navigateHistory(delta int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if len(a.cmdHistory) == 0 { return }
	if a.historyIdx == -1 && delta == -1 {
		a.historyIdx = len(a.cmdHistory) - 1
	} else {
		newIdx := a.historyIdx + delta
		if newIdx >= 0 && newIdx < len(a.cmdHistory) {
			a.historyIdx = newIdx
		} else if newIdx >= len(a.cmdHistory) {
			a.historyIdx = -1
			a.inputBuffer = ""; a.cursorIdx = 0; return
		} else { return }
	}
	if a.historyIdx != -1 {
		a.inputBuffer = a.cmdHistory[a.historyIdx]
		a.cursorIdx = len([]rune(a.inputBuffer))
	}
}

func (a *App) handleTab() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	line := a.inputBuffer
	parts := strings.Fields(line)
	if line == "" || strings.HasSuffix(line, " ") { return }
	searchTerm := parts[len(parts)-1]
	var candidates []string
	if len(parts) == 1 {
		pathEnv := os.Getenv("PATH")
		for _, dir := range filepath.SplitList(pathEnv) {
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				if strings.HasPrefix(f.Name(), searchTerm) { candidates = append(candidates, f.Name()) }
			}
		}
	}
	files, _ := os.ReadDir(a.cwd)
	for _, f := range files {
		name := f.Name()
		if f.IsDir() { name += "/" }
		if strings.HasPrefix(name, searchTerm) { candidates = append(candidates, name) }
	}
	candidates = uniqueStrings(candidates)
	if len(candidates) == 1 {
		newLine := line[:len(line)-len(searchTerm)] + candidates[0]
		a.inputBuffer = newLine
		a.cursorIdx = len([]rune(a.inputBuffer))
	} else if len(candidates) > 1 {
		common := longestCommonPrefix(candidates)
		if len(common) > len(searchTerm) {
			a.inputBuffer = line[:len(line)-len(searchTerm)] + common
			a.cursorIdx = len([]rune(a.inputBuffer))
		}
		a.history = append(a.history, strings.Join(candidates, "  "))
	}
}

func uniqueStrings(slice []string) []string {
	m := make(map[string]bool); var result []string
	for _, s := range slice { if !m[s] { m[s] = true; result = append(result, s) } }
	return result
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 { return "" }
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" { return "" }
		}
	}
	return prefix
}

func (a *App) handleCommand() {
	a.mutex.Lock()
	input := strings.TrimSpace(a.inputBuffer)
	a.inputBuffer = ""; a.cursorIdx = 0; a.historyIdx = -1
	if input == "" { a.mutex.Unlock(); return }
	if len(a.cmdHistory) == 0 || a.cmdHistory[len(a.cmdHistory)-1] != input {
		a.cmdHistory = append(a.cmdHistory, input)
	}
	fullPrompt := fmt.Sprintf("%s:%s$ %s", a.ps1, a.getFormattedDir(), input)
	a.history = append(a.history, fullPrompt)
	a.mutex.Unlock()

	args := strings.Fields(input)
	if args[0] == "cd" {
		target := ""
		if len(args) > 1 { target = args[1] } else { target, _ = os.UserHomeDir() }
		err := os.Chdir(target)
		a.mutex.Lock()
		if err != nil { a.history = append(a.history, err.Error()) } else { a.cwd, _ = os.Getwd() }
		a.mutex.Unlock()
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", input)
	} else {
		cmd = exec.Command(shell, "-c", input)
	}
	cmd.Dir = a.cwd
	out, err := cmd.CombinedOutput()

	a.mutex.Lock()
	if err != nil && len(out) == 0 { a.history = append(a.history, "Error: "+err.Error()) }
	resLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range resLines {
		if line != "" { a.history = append(a.history, line) }
	}
	a.mutex.Unlock()
}

func (a *App) getFormattedDir() string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(a.cwd, home) { return strings.Replace(a.cwd, home, "~", 1) }
	return a.cwd
}

func (a *App) runPeriodicCommand() {
	var out []byte
	outputs := ""
	for _, cmd := range monitorCommands {
		if runtime.GOOS == "windows" {
			out, _ = exec.Command("cmd", "/C", cmd.COMMAND).CombinedOutput()
		} else {
			out, _ = exec.Command(shell, "-c", cmd.COMMAND).CombinedOutput()
		}
		outputs = outputs + ExtractErrorLines(out, cmd.LABEL, cmd.COMMAND)
	}
	a.mutex.Lock()
	a.upperContent = outputs
	a.mutex.Unlock()
	a.draw()
}

func ExtractErrorLines(data []byte, Label, Command string) string {
	result := ""
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		strLine := string(line)
		if strings.Contains(strLine, Label) { result = result + "[" + Command + "] " + strLine + "\n" }
	}
	return result
}

func (a *App) draw() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()
	separatorY := h / 2

	// 上部
	uLines := strings.Split(a.upperContent, "\n")
	for i, line := range uLines {
		if i >= separatorY { break }
		printString(0, i, truncate(line, w), termbox.ColorCyan, termbox.ColorDefault)
	}
	for x := 0; x < w; x++ { termbox.SetCell(x, separatorY, '-', termbox.ColorYellow, termbox.ColorDefault) }

	// 下部
	historyHeight := (h - 1) - (separatorY + 1)
	startIdx := 0
	if len(a.history) > historyHeight { startIdx = len(a.history) - historyHeight }
	for i := 0; i < historyHeight && (startIdx+i) < len(a.history); i++ {
		printString(0, separatorY+1+i, truncate(a.history[startIdx+i], w), termbox.ColorWhite, termbox.ColorDefault)
	}

	// プロンプト
	promptPrefix := fmt.Sprintf("%s:%s$ ", a.ps1, a.getFormattedDir())
	promptY := h - 1
	printString(0, promptY, promptPrefix, termbox.ColorGreen, termbox.ColorDefault)
	printString(len(promptPrefix), promptY, a.inputBuffer, termbox.ColorWhite, termbox.ColorDefault)

	// ポップアップの描画
	if a.showPopup {
		a.drawPopup(w, h)
		termbox.HideCursor() // ポップアップ中はカーソルを隠す
	} else {
		termbox.SetCursor(len(promptPrefix)+a.cursorIdx, promptY)
	}
	
	termbox.Flush()
}

func printString(x, y int, str string, fg, bg termbox.Attribute) {
	for i, ch := range str { termbox.SetCell(x+i, y, ch, fg, bg) }
}

func truncate(s string, w int) string {
	if len(s) <= w { return s }
	return s[:w]
}

func loadConfig(configFile string) bool {
	fp, err := os.Open(configFile)
	if err != nil { return false }
	defer fp.Close()
	reader := csv.NewReader(fp)
	reader.Comma = '\t'
	reader.LazyQuotes = true
	for {
		record, err := reader.Read()
		if err == io.EOF { break } else if err != nil { return false }
		if len(record) == 2 {
			monitorCommands = append(monitorCommands, monitorCommand{LABEL: record[0], COMMAND: record[1]})
		}
	}
	return monitorCommands != nil
}
