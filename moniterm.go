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
	history      []string // 画面表示用のログ（プロンプトや出力含む）
	cmdHistory   []string // 【追加】実行したコマンドのみの履歴
	historyIdx   int      // 【追加】現在の履歴参照位置
	cwd          string
	ps1          string
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
	_interval := flag.Int("interval", 10, "[-int=Command check interval]")
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
		cmdHistory:   []string{}, // 初期化
		historyIdx:   -1,         // -1は履歴を参照していない状態
		cwd:          cwd,
		ps1:          fmt.Sprintf("%s@%s", u.Username, h),
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
			if ev.Key == termbox.KeyCtrlC || ev.Key == termbox.KeyEsc {
				return
			} else if ev.Key == termbox.KeyEnter {
				app.handleCommand()
			} else if ev.Key == termbox.KeyTab {
				app.handleTab()
			} else if ev.Key == termbox.KeyArrowUp {
				app.navigateHistory(-1) // 【追加】過去へ
			} else if ev.Key == termbox.KeyArrowDown {
				app.navigateHistory(1) // 【追加】未来へ
			} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
				app.mutex.Lock()
				if len(app.inputBuffer) > 0 {
					r := []rune(app.inputBuffer)
					app.inputBuffer = string(r[:len(r)-1])
					app.historyIdx = -1 // 入力が変わったら履歴参照を解除
				}
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeySpace {
				app.mutex.Lock()
				app.inputBuffer += " "
				app.mutex.Unlock()
			} else if ev.Ch != 0 {
				app.mutex.Lock()
				app.inputBuffer += string(ev.Ch)
				app.historyIdx = -1 // 入力が変わったら履歴参照を解除
				app.mutex.Unlock()
			}
		case termbox.EventResize:
		case termbox.EventError:
			panic(ev.Err)
		}
		app.draw()
	}
}

// 【追加】履歴移動ロジック
func (a *App) navigateHistory(delta int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if len(a.cmdHistory) == 0 {
		return
	}

	// 履歴参照の開始
	if a.historyIdx == -1 && delta == -1 {
		a.historyIdx = len(a.cmdHistory) - 1
	} else {
		newIdx := a.historyIdx + delta
		if newIdx >= 0 && newIdx < len(a.cmdHistory) {
			a.historyIdx = newIdx
		} else if newIdx >= len(a.cmdHistory) {
			a.historyIdx = -1
			a.inputBuffer = ""
			return
		} else {
			return
		}
	}

	if a.historyIdx != -1 {
		a.inputBuffer = a.cmdHistory[a.historyIdx]
	}
}

func (a *App) handleTab() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	line := a.inputBuffer
	parts := strings.Fields(line)

	if line == "" || strings.HasSuffix(line, " ") {
		return
	}

	searchTerm := parts[len(parts)-1]
	var candidates []string

	if len(parts) == 1 {
		pathEnv := os.Getenv("PATH")
		for _, dir := range filepath.SplitList(pathEnv) {
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				if strings.HasPrefix(f.Name(), searchTerm) {
					candidates = append(candidates, f.Name())
				}
			}
		}
	}

	files, _ := os.ReadDir(a.cwd)
	for _, f := range files {
		name := f.Name()
		if f.IsDir() {
			name += "/"
		}
		if strings.HasPrefix(name, searchTerm) {
			candidates = append(candidates, name)
		}
	}

	candidates = uniqueStrings(candidates)

	if len(candidates) == 0 {
		return
	} else if len(candidates) == 1 {
		newLine := line[:len(line)-len(searchTerm)] + candidates[0]
		a.inputBuffer = newLine
	} else {
		common := longestCommonPrefix(candidates)
		if len(common) > len(searchTerm) {
			a.inputBuffer = line[:len(line)-len(searchTerm)] + common
		}
		a.history = append(a.history, strings.Join(candidates, "  "))
	}
}

func uniqueStrings(slice []string) []string {
	m := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !m[s] {
			m[s] = true
			result = append(result, s)
		}
	}
	return result
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

func (a *App) handleCommand() {
	a.mutex.Lock()
	input := strings.TrimSpace(a.inputBuffer)
	a.inputBuffer = ""
	a.historyIdx = -1 // 履歴参照をリセット

	if input == "" {
		a.mutex.Unlock()
		return
	}

	// コマンド履歴に追加（重複しなければ）
	if len(a.cmdHistory) == 0 || a.cmdHistory[len(a.cmdHistory)-1] != input {
		a.cmdHistory = append(a.cmdHistory, input)
	}

	fullPrompt := fmt.Sprintf("%s:%s$ %s", a.ps1, a.getFormattedDir(), input)
	a.history = append(a.history, fullPrompt)
	a.mutex.Unlock()

	args := strings.Fields(input)
	if args[0] == "cd" {
		target := ""
		if len(args) > 1 {
			target = args[1]
		} else {
			target, _ = os.UserHomeDir()
		}
		err := os.Chdir(target)
		a.mutex.Lock()
		if err != nil {
			a.history = append(a.history, err.Error())
		} else {
			a.cwd, _ = os.Getwd()
		}
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
	if err != nil && len(out) == 0 {
		a.history = append(a.history, "Error: "+err.Error())
	}
	resLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range resLines {
		if line != "" {
			a.history = append(a.history, line)
		}
	}
	a.mutex.Unlock()
}

func (a *App) getFormattedDir() string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(a.cwd, home) {
		return strings.Replace(a.cwd, home, "~", 1)
	}
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
		if strings.Contains(strLine, Label) {
			result = result + "[" + Command + "] " + strLine + "\n"
		}
	}
	return result
}

func (a *App) draw() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()
	separatorY := h / 2

	uLines := strings.Split(a.upperContent, "\n")
	for i, line := range uLines {
		if i >= separatorY {
			break
		}
		printString(0, i, truncate(line, w), termbox.ColorCyan, termbox.ColorDefault)
	}
	for x := 0; x < w; x++ {
		termbox.SetCell(x, separatorY, '-', termbox.ColorYellow, termbox.ColorDefault)
	}

	historyHeight := (h - 1) - (separatorY + 1)
	startIdx := 0
	if len(a.history) > historyHeight {
		startIdx = len(a.history) - historyHeight
	}
	for i := 0; i < historyHeight && (startIdx+i) < len(a.history); i++ {
		printString(0, separatorY+1+i, truncate(a.history[startIdx+i], w), termbox.ColorWhite, termbox.ColorDefault)
	}

	promptPrefix := fmt.Sprintf("%s:%s$ ", a.ps1, a.getFormattedDir())
	promptY := h - 1
	printString(0, promptY, promptPrefix, termbox.ColorGreen, termbox.ColorDefault)
	printString(len(promptPrefix), promptY, a.inputBuffer, termbox.ColorWhite, termbox.ColorDefault)
	termbox.SetCursor(len(promptPrefix)+len(a.inputBuffer), promptY)
	termbox.Flush()
}

func printString(x, y int, str string, fg, bg termbox.Attribute) {
	for i, ch := range str {
		termbox.SetCell(x+i, y, ch, fg, bg)
	}
}

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	return s[:w]
}

func loadConfig(configFile string) bool {
	fp, err := os.Open(configFile)
	if err != nil {
		return false
	}
	defer fp.Close()

	reader := csv.NewReader(fp)
	reader.Comma = '\t'
	reader.LazyQuotes = true
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return false
		}
		if len(record) == 2 {
			monitorCommands = append(monitorCommands, monitorCommand{LABEL: record[0], COMMAND: record[1]})
		}
	}
	return monitorCommands != nil
}