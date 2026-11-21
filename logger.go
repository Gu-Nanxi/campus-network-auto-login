package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Logger 是最小可用的滚动日志：超过 max 就改名为 .1 重新开始。
//
// 每次写都开-写-关，不长期占着文件句柄：
// 这样 `-mode status` 可以在常驻进程运行时照样读日志。
type Logger struct {
	mu   sync.Mutex
	path string
	max  int64
}

func newLogger(path string) *Logger {
	return &Logger{path: path, max: 1 << 20} // 1 MiB
}

func (l *Logger) write(level, format string, args ...any) {
	line := fmt.Sprintf("%s [%s] %s", time.Now().Format("2006-01-02 15:04:05"), level, fmt.Sprintf(format, args...))

	l.mu.Lock()
	defer l.mu.Unlock()

	// 控制台也打一份；没有控制台时写失败，忽略即可
	_, _ = os.Stdout.WriteString(line + "\n")

	if l.path == "" {
		return
	}
	if st, err := os.Stat(l.path); err == nil && st.Size() > l.max {
		_ = os.Remove(l.path + ".1")
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			// 轮转失败就继续往后追加，总比丢日志好
			_ = err
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

func (l *Logger) Info(format string, args ...any)  { l.write("INFO", format, args...) }
func (l *Logger) Warn(format string, args ...any)  { l.write("WARN", format, args...) }
func (l *Logger) Error(format string, args ...any) { l.write("ERROR", format, args...) }

// Close 保留给调用方统一 defer，实际没有需要释放的资源。
func (l *Logger) Close() {}
