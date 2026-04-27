package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/nsf/termbox-go"
)

type App struct {
	upperContent string
	inputBuffer  string
	history      []string
	cwd          string // カレントディレクトリ保持用
	ps1          string // user@hostname 部分
	mutex        sync.Mutex
}

func main() {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()

	// Bash風プロンプトのための情報を取得
	u, _ := user.Current()
	h, _ := os.Hostname()
	cwd, _ := os.Getwd()

	app := &App{
		upperContent: "Command output will appear here...",
		history:      []string{"Welcome to the Custom Terminal!"},
		cwd:          cwd,
		ps1:          fmt.Sprintf("%s@%s", u.Username, h),
	}

	app.draw()

	// 上画面：10秒ごとにコマンド実行
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		app.runPeriodicCommand()
		for range ticker.C {
			app.runPeriodicCommand()
		}
	}()

	// イベントループ
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			if ev.Key == termbox.KeyCtrlC || ev.Key == termbox.KeyEsc {
				return
			} else if ev.Key == termbox.KeyEnter {
				app.handleCommand()
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

// カレントディレクトリをbash風に短縮 (~/...)
func (a *App) getFormattedDir() string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(a.cwd, home) {
		return strings.Replace(a.cwd, home, "~", 1)
	}
	return a.cwd
}

func (a *App) handleCommand() {
	a.mutex.Lock()
	input := strings.TrimSpace(a.inputBuffer)
	a.inputBuffer = ""

	if input == "" {
		a.mutex.Unlock()
		return
	}

	// 履歴にプロンプト付きで残す
	fullPrompt := fmt.Sprintf("%s:%s$ %s", a.ps1, a.getFormattedDir(), input)
	a.history = append(a.history, fullPrompt)
	a.mutex.Unlock()

	// 特殊処理: cd コマンド
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

	// 一般コマンドの実行
	cmd := exec.Command("/bin/bash", "-c", input)
	cmd.Dir = a.cwd // アプリが保持しているディレクトリで実行
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

func (a *App) runPeriodicCommand() {
	out, _ := exec.Command("uptime").Output()
	a.mutex.Lock()
	a.upperContent = fmt.Sprintf("Last Update: %s\n%s", time.Now().Format("15:04:05"), string(out))
	a.mutex.Unlock()
	a.draw()
}

func (a *App) draw() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()
	separatorY := h / 2

	// 上部描画
	uLines := strings.Split(a.upperContent, "\n")
	for i, line := range uLines {
		if i >= separatorY { break }
		printString(0, i, truncate(line, w), termbox.ColorCyan, termbox.ColorDefault)
	}

	// 境界線
	for x := 0; x < w; x++ {
		termbox.SetCell(x, separatorY, '-', termbox.ColorYellow, termbox.ColorDefault)
	}

	// 下部（履歴）描画
	historyHeight := (h - 1) - (separatorY + 1)
	startIdx := 0
	if len(a.history) > historyHeight {
		startIdx = len(a.history) - historyHeight
	}
	for i := 0; i < historyHeight && (startIdx+i) < len(a.history); i++ {
		printString(0, separatorY+1+i, truncate(a.history[startIdx+i], w), termbox.ColorWhite, termbox.ColorDefault)
	}

	// プロンプト（最下行）の描画
	dirPart := a.getFormattedDir()
	promptPrefix := fmt.Sprintf("%s:%s$ ", a.ps1, dirPart)
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
	if len(s) <= w { return s }
	return s[:w]
}