package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"flag"
	"log"
	"encoding/csv"
	"io"
	"bytes"

	"github.com/nsf/termbox-go"
)

type App struct {
	upperContent string
	inputBuffer  string
	history      []string
	cwd          string
	ps1          string
	mutex        sync.Mutex
}

type monitorCommand struct {
  LABEL  string
  COMMAND string
}

var (
	monitorCommands []monitorCommand
	shell			string
)

func main() {
	_interval := flag.Int("interval",10,"[-int=Command check interval]")
    _config   := flag.String("config","moniterm.ini","[-config=Config filename]")
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
				app.handleTab() // 【追加】タブ補完
			} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
				app.mutex.Lock()
				if len(app.inputBuffer) > 0 {
					r := []rune(app.inputBuffer)
					app.inputBuffer = string(r[:len(r)-1])
				}
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeySpace {
				app.mutex.Lock()
				app.inputBuffer += " "
				app.mutex.Unlock()
			} else if ev.Ch != 0 {
				app.mutex.Lock()
				app.inputBuffer += string(ev.Ch)
				app.mutex.Unlock()
			}
		case termbox.EventResize:
		case termbox.EventError:
			panic(ev.Err)
		}
		app.draw()
	}
}

// 【追加】タブ補完ロジック
func (a *App) handleTab() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	line := a.inputBuffer
	parts := strings.Fields(line)
	
	// 入力が空、または末尾がスペースの場合は補完対象なしとする（簡易化）
	if line == "" || strings.HasSuffix(line, " ") {
		return
	}

	searchTerm := parts[len(parts)-1]
	var candidates []string

	if len(parts) == 1 {
		// 最初の単語：PATHからコマンドを探す
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
	
	// カレントディレクトリのファイルも常に候補に含める
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

	// 重複削除
	candidates = uniqueStrings(candidates)

	if len(candidates) == 0 {
		return
	} else if len(candidates) == 1 {
		// 唯一の候補なら即補完
		newLine := line[:len(line)-len(searchTerm)] + candidates[0]
		a.inputBuffer = newLine
	} else {
		// 複数候補：共通部分まで補完し、候補を履歴に表示
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

// (以下、以前のロジックと同様)
func (a *App) handleCommand() {
	a.mutex.Lock()
	input := strings.TrimSpace(a.inputBuffer)
	a.inputBuffer = ""
	if input == "" {
		a.mutex.Unlock()
		return
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

	cmd := exec.Command(shell, "-c", input)
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
	outputs := ""

	for _, cmd := range monitorCommands {
		out, _ := exec.Command(shell, "-c", cmd.COMMAND).CombinedOutput()
		outputs = outputs + ExtractErrorLines(out, cmd.LABEL)
	}
	
	a.mutex.Lock()
	a.upperContent = outputs
	a.mutex.Unlock()
	a.draw()
}

// ExtractErrorLines は []byte を受け取り、"Error" を含む行を []string で返します
func ExtractErrorLines(data []byte, Label string) string {
	result := ""

	// 1. []byte を改行で分割する
	// bytes.Split は []byte を保持したまま分割するためメモリ効率が良いです
	lines := bytes.Split(data, []byte("\n"))

	for _, line := range lines {
		// 2. 各行を文字列に変換
		strLine := string(line)

		// 3. "Error" が含まれているかチェック
		if strings.Contains(strLine, Label) {
			result = result + strLine + "\n"
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
		if i >= separatorY { break }
		printString(0, i, truncate(line, w), termbox.ColorCyan, termbox.ColorDefault)
	}
	for x := 0; x < w; x++ { termbox.SetCell(x, separatorY, '-', termbox.ColorYellow, termbox.ColorDefault) }

	historyHeight := (h - 1) - (separatorY + 1)
	startIdx := 0
	if len(a.history) > historyHeight { startIdx = len(a.history) - historyHeight }
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
	for i, ch := range str { termbox.SetCell(x+i, y, ch, fg, bg) }
}

func truncate(s string, w int) string {
	if len(s) <= w { return s }
	return s[:w]
}

func loadConfig(configFile string) bool {
	var fp *os.File
	var err error
	fp, err = os.Open(configFile)
	if err != nil {
		panic(err)
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
			panic(err)
		}
		if len(record) == 2 {
			monitorCommands = append(monitorCommands, monitorCommand{LABEL: record[0], COMMAND: record[1]})
			fmt.Println(record)
		}
	}
	if monitorCommands == nil {
		return false
	}
	return true
}