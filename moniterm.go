package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nsf/termbox-go"
)

// アプリケーションの状態管理
type App struct {
	upperContent string
	inputBuffer  string
	mutex        sync.Mutex
}

func main() {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()

	app := &App{
		upperContent: "Command output will appear here...",
	}

	// 初回の描画
	app.draw()

	// 1. 定期的にコマンドを実行するゴルーチン (10秒ごと)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		// 初回実行
		app.runPeriodicCommand()
		
		for range ticker.C {
			app.runPeriodicCommand()
		}
	}()

	// 2. メインイベントループ (ユーザー入力受付)
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			if ev.Key == termbox.KeyCtrlC || ev.Key == termbox.KeyEsc {
				return
			} else if ev.Key == termbox.KeyEnter {
				// Enterが押されたら入力をクリア（ここで入力コマンドの処理も可能）
				app.mutex.Lock()
				app.inputBuffer = ""
				app.mutex.Unlock()
			} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
				app.mutex.Lock()
				if len(app.inputBuffer) > 0 {
					app.inputBuffer = app.inputBuffer[:len(app.inputBuffer)-1]
				}
				app.mutex.Unlock()
			} else if ev.Ch != 0 {
				app.mutex.Lock()
				app.inputBuffer += string(ev.Ch)
				app.mutex.Unlock()
			}
		case termbox.EventResize:
			// 画面サイズ変更時
		case termbox.EventError:
			panic(ev.Err)
		}
		app.draw()
	}
}

// 特定のコマンド（例: uptime）を実行して結果を保存
func (a *App) runPeriodicCommand() {
	// 実行するコマンドをここで指定
	//out, err := exec.Command("cat /var/log/kern.log").Output()
	out, err := exec.Command("/bin/bash", "-c", "cat /var/log/kern.log").Output()
	
	a.mutex.Lock()
	if err != nil {
		a.upperContent = fmt.Sprintf("Error: %v", err)
	} else {
		a.upperContent = fmt.Sprintf("Last Update: %s\n%s", 
			time.Now().Format("15:04:05"), string(out))
	}
	a.mutex.Unlock()
	
	// 描画更新
	a.draw()
}

// 画面描画処理
func (a *App) draw() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()

	// 境界線の行を計算
	separatorY := h - 3

	// --- 上部画面の描画 ---
	lines := strings.Split(a.upperContent, "\n")
	for i, line := range lines {
		if i >= separatorY {
			break
		}
		printString(0, i, line, termbox.ColorCyan, termbox.ColorDefault)
	}

	// --- 境界線の描画 ---
	for x := 0; x < w; x++ {
		termbox.SetCell(x, separatorY, '-', termbox.ColorWhite, termbox.ColorDefault)
	}

	// --- 下部画面（プロンプト）の描画 ---
	prompt := "> "
	printString(0, h-2, prompt+a.inputBuffer, termbox.ColorWhite, termbox.ColorDefault)

	// カーソルの位置を入力の末尾にセット
	termbox.SetCursor(len(prompt)+len(a.inputBuffer), h-2)

	termbox.Flush()
}

// 文字列を描画するヘルパー関数
func printString(x, y int, str string, fg, bg termbox.Attribute) {
	for i, ch := range str {
		termbox.SetCell(x+i, y, ch, fg, bg)
	}
}