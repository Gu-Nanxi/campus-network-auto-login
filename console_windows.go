//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procAttachConsole      = kernel32.NewProc("AttachConsole")
	procAllocConsole       = kernel32.NewProc("AllocConsole")
	procSetConsoleTitleW   = kernel32.NewProc("SetConsoleTitleW")
	procGetStdHandle       = kernel32.NewProc("GetStdHandle")
	procSetStdHandle       = kernel32.NewProc("SetStdHandle")
	procGetConsoleMode     = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode     = kernel32.NewProc("SetConsoleMode")
	procGetFileType        = kernel32.NewProc("GetFileType")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procCreateMutexW       = kernel32.NewProc("CreateMutexW")
	procCloseHandle        = kernel32.NewProc("CloseHandle")

	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	attachParentProcess = ^uintptr(0) // (DWORD)-1，附着到父进程的控制台

	stdInputHandle  = uintptr(0xFFFFFFF6)
	stdOutputHandle = uintptr(0xFFFFFFF5)
	stdErrorHandle  = uintptr(0xFFFFFFF4)

	enableEchoInput = 0x0004
)

var consoleReader *bufio.Reader

// ensureConsole 让进程拿到可用的控制台：
//
//  1. 已经有控制台（比如从 cmd 里启动）→ 直接接管
//  2. 父进程有控制台 → 附着上去（windowsgui 的 exe 从终端启动时走这条）
//  3. allowCreate 为真 → 新建一个黑框（双击 exe 首次设置时走这条）
//  4. 都不行 → false，静默运行
func ensureConsole(allowCreate bool) bool {
	// 输出已经被重定向到管道/文件（CI、`> log.txt`、被别的程序捕获）：
	// 这时候千万不要抢回控制台，否则内容会跑到屏幕而不是该去的地方。
	if stdioRedirected() {
		return true
	}
	// 探测当前是否已经挂着控制台
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		f.Close()
		retargetStd()
		return true
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r != 0 {
		retargetStd()
		return true
	}
	if !allowCreate {
		return false
	}
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		return false
	}
	retargetStd()
	setConsoleTitle("CampusAutoLogin")
	return true
}

// stdioRedirected 判断 stdout 是否已经被重定向到文件或管道。
// GetFileType: 1=磁盘文件 2=字符设备（控制台） 3=管道 0=未知（句柄无效）
func stdioRedirected() bool {
	h, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if h == 0 || h == ^uintptr(0) {
		return false
	}
	t, _, _ := procGetFileType.Call(h)
	return t == 1 || t == 3
}

// retargetStd 在拿到控制台之后重新绑定标准流。
// windowsgui 子系统的进程启动时标准句柄是无效的，必须换成 CONOUT$ / CONIN$。
func retargetStd() {
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		// 控制台默认是 GBK 代码页，Go 写的是 UTF-8，不切成 65001 中文会变乱码。
		// 附着到别人的控制台时记下原代码页，退出时还原，别把人家终端搞花。
		if cp, _, _ := procGetConsoleOutputCP.Call(); cp != 0 && cp != 65001 {
			prevConsoleCP = uint32(cp)
			cpChanged = true
		}
		procSetConsoleOutputCP.Call(65001)
		procSetConsoleCP.Call(65001)
		os.Stdout = f
		os.Stderr = f
		procSetStdHandle.Call(stdOutputHandle, f.Fd())
		procSetStdHandle.Call(stdErrorHandle, f.Fd())
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
		procSetStdHandle.Call(stdInputHandle, f.Fd())
	}
	consoleReader = nil
}

var (
	prevConsoleCP uint32
	cpChanged     bool
)

// restoreConsoleCP 退出前把控制台代码页还回去。
func restoreConsoleCP() {
	if !cpChanged {
		return
	}
	procSetConsoleOutputCP.Call(uintptr(prevConsoleCP))
	procSetConsoleCP.Call(uintptr(prevConsoleCP))
	cpChanged = false
}

func setConsoleTitle(s string) {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
}

const mbIconInformation = 0x40

// alert 在没有控制台时用系统弹窗兜底，避免"双击了但什么也没发生"。
func alert(title, text string) {
	t, err1 := syscall.UTF16PtrFromString(title)
	x, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(x)), uintptr(unsafe.Pointer(t)), mbIconInformation)
}

// 弹窗返回值
const (
	msgYes    = 6
	msgNo     = 7
	msgCancel = 2
)

// alertChoice 问一个"是 / 否 / 取消"的问题，返回点的是哪个按钮。
// 用户关掉窗口或者按 Esc 都算取消。
func alertChoice(title, text string) int {
	t, err1 := syscall.UTF16PtrFromString(title)
	x, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return msgCancel
	}
	const mbYesNoCancel = 0x3
	const mbIconQuestion = 0x20
	const mbSetForeground = 0x10000
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(x)), uintptr(unsafe.Pointer(t)),
		mbYesNoCancel|mbIconQuestion|mbSetForeground)
	return int(r)
}

func stdinReader() *bufio.Reader {
	if consoleReader == nil {
		consoleReader = bufio.NewReader(os.Stdin)
	}
	return consoleReader
}

func readLine() string {
	s, err := stdinReader().ReadString('\n')
	if err != nil && s == "" {
		return ""
	}
	return strings.TrimRight(s, "\r\n")
}

// readSecret 读密码：关掉控制台的输入回显，读完再恢复。
// 输入被重定向（没有控制台）时退化成普通读行，不会卡住。
func readSecret(prompt string) string {
	fmt.Print(prompt)
	h, _, _ := procGetStdHandle.Call(stdInputHandle)
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
		procSetConsoleMode.Call(h, uintptr(mode&^enableEchoInput))
		defer procSetConsoleMode.Call(h, uintptr(mode))
	}
	defer fmt.Println()
	return readLine()
}

func askYesNo(prompt string, defYes bool) bool {
	fmt.Print(prompt)
	s := strings.ToLower(strings.TrimSpace(readLine()))
	if s == "" {
		return defYes
	}
	return s == "y" || s == "yes" || s == "是" || s == "1"
}

// singleInstance 防止开机自启和手动双击同时跑出两个进程、互相抢着登录。
func singleInstance() (release func(), ok bool) {
	name, err := syscall.UTF16PtrFromString(`Local\CampusAutoLogin`)
	if err != nil {
		return func() {}, true
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return func() {}, true
	}
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		procCloseHandle.Call(h)
		return func() {}, false
	}
	return func() { procCloseHandle.Call(h) }, true
}
