//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// 停止信号：一个命名事件。常驻进程在里面挂一个 goroutine 等它，
// 别的进程（双击弹窗、-mode stop、停止运行.cmd）往上面 SetEvent 就能让它干净退出。
// 比"按名字杀进程"安全：不会误杀同名的别人的程序，也能让程序自己先把日志写完。
const stopEventName = `Local\CampusAutoLoginStop`

var (
	procCreateEventW        = kernel32.NewProc("CreateEventW")
	procOpenEventW          = kernel32.NewProc("OpenEventW")
	procSetEvent            = kernel32.NewProc("SetEvent")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

const (
	eventModifyState = 0x0002
	infiniteWait     = 0xFFFFFFFF
)

// watchStop 在常驻进程启动时调用；收到停止信号就执行 onStop。
func watchStop(onStop func()) {
	name, err := syscall.UTF16PtrFromString(stopEventName)
	if err != nil {
		return
	}
	// 自动复位事件，初始未触发
	h, _, _ := procCreateEventW.Call(0, 0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return
	}
	go func() {
		procWaitForSingleObject.Call(h, infiniteWait)
		onStop()
	}()
}

// signalStop 通知正在运行的实例退出。返回 false 表示没有实例在等这个信号。
func signalStop() bool {
	name, err := syscall.UTF16PtrFromString(stopEventName)
	if err != nil {
		return false
	}
	h, _, _ := procOpenEventW.Call(eventModifyState, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	r, _, _ := procSetEvent.Call(h)
	return r != 0
}

// processGone 判断常驻实例是否已经退出（重试若干次，给它把日志写完的时间）。
func processGone() bool {
	for i := 0; i < 40; i++ {
		if !instanceRunning() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !instanceRunning()
}
