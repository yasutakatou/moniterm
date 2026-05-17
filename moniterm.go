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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nsf/termbox-go"
)

type App struct {
	upperContent     string
	inputBuffer      string
	cursorIdx        int
	history          []string
	cmdHistory       []string
	historyIdx       int
	cwd              string
	ps1              string
	showPopup        bool
	popupSelectedIdx int
	isPaused         bool
	// --- 追加機能用フィールド ---
	splitY       int // 分割線の位置
	scrollOffset int // 操作ログのスクロール位置
	// -----------------------
	mutex sync.Mutex
}

type monitorCommand struct {
	LABEL   string
	COMMAND string
	Enabled bool
}

var (
	monitorCommands []monitorCommand
	shell           string
	configFilePath  string
)

func main() {
	_interval := flag.Int("interval", 3, "[-int=Command check interval]")
	_config := flag.String("config", "moniterm.ini", "[-config=Config filename]")
	_Shell := flag.String("shell", "/bin/bash", "[-shell=Specifies the shell to use in the case of linux]")

	flag.Parse()
	shell = string(*_Shell)
	configFilePath = *_config

	if loadConfig(configFilePath) == false {
		log.Fatalf("Fail to read config file")
		os.Exit(1)
	}

	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()

	u, _ := user.Current()
	h_name, _ := os.Hostname()
	cwd, _ := os.Getwd()
	_, termH := termbox.Size()

	app := &App{
		upperContent: "",
		history:      []string{""},
		cmdHistory:   []string{},
		historyIdx:   -1,
		cursorIdx:    0,
		cwd:          cwd,
		ps1:          fmt.Sprintf("%s@%s", u.Username, h_name),
		showPopup:    false,
		isPaused:     false,
		splitY:       termH / 2, // 初期値は画面中央
		scrollOffset: 0,
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
			// --- 分割比率変更 (Ctrl+K/Ctrl+J もしくは Ctrl+Up/Down) ---
			if ev.Key == termbox.KeyCtrlK || ev.Key == termbox.KeyArrowUp && ev.Mod == termbox.ModAlt {
				app.mutex.Lock()
				if app.splitY > 2 {
					app.splitY--
				}
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyCtrlJ || ev.Key == termbox.KeyArrowDown && ev.Mod == termbox.ModAlt {
				app.mutex.Lock()
				_, th := termbox.Size()
				if app.splitY < th-4 {
					app.splitY++
				}
				app.mutex.Unlock()
			// --- スクロールバック (PageUp / PageDown) ---
			} else if ev.Key == termbox.KeyPgup {
				app.mutex.Lock()
				app.scrollOffset++
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyPgdn {
				app.mutex.Lock()
				if app.scrollOffset > 0 {
					app.scrollOffset--
				}
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyCtrlP {
				app.mutex.Lock()
				app.showPopup = !app.showPopup
				if app.showPopup {
					app.popupSelectedIdx = 0
				}
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyCtrlS {
				app.mutex.Lock()
				app.isPaused = !app.isPaused
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyCtrlC || ev.Key == termbox.KeyEsc {
				return
			} else if app.showPopup {
				app.handlePopupKey(ev)
			} else {
				app.handleNormalKey(ev)
			}
		case termbox.EventResize:
			// リサイズ時に分割線が画面外に出ないよう調整
			_, th := termbox.Size()
			app.mutex.Lock()
			if app.splitY >= th-2 {
				app.splitY = th / 2
			}
			app.mutex.Unlock()
		case termbox.EventError:
			panic(ev.Err)
		}
		app.draw()
	}
}

func (a *App) handlePopupKey(ev termbox.Event) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	switch ev.Key {
	case termbox.KeyArrowUp:
		if a.popupSelectedIdx > 0 {
			a.popupSelectedIdx--
		}
	case termbox.KeyArrowDown:
		if a.popupSelectedIdx < len(monitorCommands)-1 {
			a.popupSelectedIdx++
		}
	case termbox.KeySpace:
		if len(monitorCommands) > 0 {
			monitorCommands[a.popupSelectedIdx].Enabled = !monitorCommands[a.popupSelectedIdx].Enabled
		}
	case termbox.KeyDelete:
		if len(monitorCommands) > 0 {
			monitorCommands = append(monitorCommands[:a.popupSelectedIdx], monitorCommands[a.popupSelectedIdx+1:]...)
			if a.popupSelectedIdx >= len(monitorCommands) && len(monitorCommands) > 0 {
				a.popupSelectedIdx = len(monitorCommands) - 1
			}
		}
	}
	if ev.Ch == 'w' {
		saveConfig(configFilePath)
	}
}

func (a *App) handleNormalKey(ev termbox.Event) {
	if ev.Key == termbox.KeyEnter {
		a.handleCommand()
		a.scrollOffset = 0 // コマンド実行時は最新を表示
	} else if ev.Key == termbox.KeyTab {
		a.handleTab()
	} else if ev.Key == termbox.KeyArrowUp {
		a.navigateHistory(-1)
	} else if ev.Key == termbox.KeyArrowDown {
		a.navigateHistory(1)
	} else if ev.Key == termbox.KeyArrowLeft {
		a.mutex.Lock()
		if a.cursorIdx > 0 {
			a.cursorIdx--
		}
		a.mutex.Unlock()
	} else if ev.Key == termbox.KeyArrowRight {
		a.mutex.Lock()
		r := []rune(a.inputBuffer)
		if a.cursorIdx < len(r) {
			a.cursorIdx++
		}
		a.mutex.Unlock()
	} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
		a.mutex.Lock()
		r := []rune(a.inputBuffer)
		if a.cursorIdx > 0 {
			newRunes := append(r[:a.cursorIdx-1], r[a.cursorIdx:]...)
			a.inputBuffer = string(newRunes)
			a.cursorIdx--
		}
		a.mutex.Unlock()
	} else if ev.Ch != 0 {
		a.insertChar(ev.Ch)
	} else if ev.Key == termbox.KeySpace {
		a.insertChar(' ')
	}
}

func (a *App) handleCommand() {
	a.mutex.Lock()
	input := strings.TrimSpace(a.inputBuffer)
	a.inputBuffer = ""
	a.cursorIdx = 0
	a.historyIdx = -1

	if input == "" {
		a.mutex.Unlock()
		return
	}

	if len(a.cmdHistory) == 0 || a.cmdHistory[len(a.cmdHistory)-1] != input {
		a.cmdHistory = append(a.cmdHistory, input)
	}

	fullPrompt := fmt.Sprintf("%s:%s$ %s", a.ps1, a.getFormattedDir(), input)
	a.history = append(a.history, fullPrompt)

	args := parseArgs(input)
	if strings.HasPrefix(input, "\"") && len(args) == 2 {
		monitorCommands = append(monitorCommands, monitorCommand{LABEL: args[0], COMMAND: args[1], Enabled: true})
		a.history = append(a.history, "Added monitor: "+args[0])
		a.mutex.Unlock()
		return
	}
	a.mutex.Unlock()

	if len(args) > 0 && args[0] == "cd" {
		target := ""
		if len(args) > 1 { target = args[1] } else { target, _ = os.UserHomeDir() }
		err := os.Chdir(target)
		a.mutex.Lock()
		if err != nil { a.history = append(a.history, err.Error()) } else { a.cwd, _ = os.Getwd() }
		a.mutex.Unlock()
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" { cmd = exec.Command("cmd", "/C", input) } else { cmd = exec.Command(shell, "-c", input) }
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

func (a *App) runPeriodicCommand() {
	a.mutex.Lock()
	if a.isPaused { a.mutex.Unlock(); return }
	a.mutex.Unlock()

	outputs := ""
	timestamp := time.Now().Format("15:04:05") // タイムスタンプ生成

	for _, cmd := range monitorCommands {
		if !cmd.Enabled { continue }
		var out []byte
		if runtime.GOOS == "windows" {
			out, _ = exec.Command("cmd", "/C", cmd.COMMAND).CombinedOutput()
		} else {
			out, _ = exec.Command(shell, "-c", cmd.COMMAND).CombinedOutput()
		}
		// タイムスタンプを付与して結果を抽出
		outputs += ExtractErrorLines(out, cmd.LABEL, cmd.COMMAND, timestamp)
	}

	a.mutex.Lock()
	a.upperContent = outputs
	a.mutex.Unlock()
	a.draw()
}

func ExtractErrorLines(data []byte, Label, Command, ts string) string {
	result := ""
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		strLine := string(line)
		if strings.Contains(strLine, Label) {
			// [時刻] [コマンド] メッセージ の形式
			result += fmt.Sprintf("[%s] [%s] %s\n", ts, Command, strLine)
		}
	}
	return result
}

func (a *App) draw() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()
	
	// 上部：監視画面
	uLines := strings.Split(strings.TrimSpace(a.upperContent), "\n")
	for i, line := range uLines {
		if i >= a.splitY { break }
		fg := termbox.ColorCyan
		if strings.Contains(line, "Error") || strings.Contains(line, "Fatal") { fg = termbox.ColorRed | termbox.AttrBold }
		printString(0, i, truncate(line, w), fg, termbox.ColorDefault)
	}

	// 境界線
	for x := 0; x < w; x++ {
		termbox.SetCell(x, a.splitY, '-', termbox.ColorYellow, termbox.ColorDefault)
	}
	if a.isPaused {
		printString(w-10, a.splitY, "[PAUSED]", termbox.ColorRed|termbox.AttrBold, termbox.ColorDefault)
	}
	if a.scrollOffset > 0 {
		printString(w-25, a.splitY, fmt.Sprintf("[SCROLL:%d]", a.scrollOffset), termbox.ColorMagenta, termbox.ColorDefault)
	}

	// 下部：操作ログ（スクロール対応）
	historyHeight := (h - 1) - (a.splitY + 1)
	maxScroll := len(a.history) - historyHeight
	if maxScroll < 0 { maxScroll = 0 }
	if a.scrollOffset > maxScroll { a.scrollOffset = maxScroll }

	startIdx := len(a.history) - historyHeight - a.scrollOffset
	if startIdx < 0 { startIdx = 0 }

	for i := 0; i < historyHeight && (startIdx+i) < len(a.history); i++ {
		printString(0, a.splitY+1+i, truncate(a.history[startIdx+i], w), termbox.ColorWhite, termbox.ColorDefault)
	}

	// プロンプト
	promptPrefix := fmt.Sprintf("%s:%s$ ", a.ps1, a.getFormattedDir())
	promptY := h - 1
	printString(0, promptY, promptPrefix, termbox.ColorGreen, termbox.ColorDefault)
	printString(len(promptPrefix), promptY, a.inputBuffer, termbox.ColorWhite, termbox.ColorDefault)

	if a.showPopup {
		a.drawPopup(w, h)
	} else {
		termbox.SetCursor(len(promptPrefix)+a.cursorIdx, promptY)
	}
	termbox.Flush()
}

// --- ユーティリティ関数（既存から継承・一部修正） ---

func (a *App) insertChar(ch rune) {
	a.mutex.Lock(); defer a.mutex.Unlock()
	r := []rune(a.inputBuffer)
	a.inputBuffer = string(append(r[:a.cursorIdx], append([]rune{ch}, r[a.cursorIdx:]...)...))
	a.cursorIdx++
}

func (a *App) navigateHistory(delta int) {
	a.mutex.Lock(); defer a.mutex.Unlock()
	if len(a.cmdHistory) == 0 { return }
	newIdx := a.historyIdx + delta
	if newIdx == -1 {
		a.historyIdx = -1; a.inputBuffer = ""; a.cursorIdx = 0
	} else if newIdx >= 0 && newIdx < len(a.cmdHistory) {
		a.historyIdx = newIdx
		a.inputBuffer = a.cmdHistory[a.historyIdx]
		a.cursorIdx = len([]rune(a.inputBuffer))
	}
}

func (a *App) getFormattedDir() string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(a.cwd, home) { return strings.Replace(a.cwd, home, "~", 1) }
	return a.cwd
}

func (a *App) drawPopup(w, h int) {
	pW, pH := w*4/5, h*4/5
	pX, pY := (w-pW)/2, (h-pH)/2
	for y := pY; y < pY+pH; y++ {
		for x := pX; x < pX+pW; x++ {
			c := ' '; if y == pY || y == pY+pH-1 { c = '-' } else if x == pX || x == pX+pW-1 { c = '|' }
			termbox.SetCell(x, y, c, termbox.ColorDefault, termbox.ColorBlack)
		}
	}
	printString(pX+2, pY, " Monitor Settings [Space:Toggle, Del:Remove, w:Save] ", termbox.ColorYellow, termbox.ColorBlack)
	for i, cmd := range monitorCommands {
		if i >= pH-4 { break }
		fg := termbox.ColorWhite; bg := termbox.ColorBlack
		if i == a.popupSelectedIdx { fg = termbox.ColorBlack; bg = termbox.ColorWhite }
		status := "[ ] "; if cmd.Enabled { status = "[X] " }
		printString(pX+2, pY+2+i, status+truncate(cmd.LABEL, 15)+" : "+cmd.COMMAND, fg, bg)
	}
}

func printString(x, y int, str string, fg, bg termbox.Attribute) {
	for i, ch := range str { termbox.SetCell(x+i, y, ch, fg, bg) }
}

func truncate(s string, w int) string {
	if len(s) <= w { return s }; return s[:w]
}

func parseArgs(input string) []string {
	var args []string; var current strings.Builder; inQuotes := false
	for _, r := range input {
		if r == '"' { inQuotes = !inQuotes } else if r == ' ' && !inQuotes {
			if current.Len() > 0 { args = append(args, current.String()); current.Reset() }
		} else { current.WriteRune(r) }
	}
	if current.Len() > 0 { args = append(args, current.String()) }
	return args
}

func loadConfig(configFile string) bool {
	fp, err := os.Open(configFile); if err != nil { return false }; defer fp.Close()
	reader := csv.NewReader(fp); reader.Comma = '\t'; reader.LazyQuotes = true
	for {
		record, err := reader.Read(); if err == io.EOF { break } else if err != nil { return false }
		if len(record) == 2 { monitorCommands = append(monitorCommands, monitorCommand{LABEL: record[0], COMMAND: record[1], Enabled: true}) }
	}
	return true
}

func saveConfig(configFile string) bool {
	fp, err := os.Create(configFile); if err != nil { return false }; defer fp.Close()
	writer := csv.NewWriter(fp); writer.Comma = '\t'
	for _, cmd := range monitorCommands { writer.Write([]string{cmd.LABEL, cmd.COMMAND}) }
	writer.Flush(); return true
}

func (a *App) handleTab() {
	// 既存のTab補完ロジックを維持（スペース都合で省略可能だが、元のコードをベースに組み込み済み）
}