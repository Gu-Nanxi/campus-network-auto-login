//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

const (
	runKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "CampusAutoLogin"
)

// 用 reg.exe 而不是自己封 advapi32：
// 少一堆 syscall 样板，行为和用户手工在注册表里操作完全一致。
func runReg(args ...string) (string, error) {
	cmd := exec.Command("reg.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// autostartEnable 写 HKCU 的 Run 键，登录 Windows 就静默启动。
// 不用任务计划：windowsgui 的 exe 从 Run 拉起不会闪黑框。
func autostartEnable(exe string) error {
	cmdline := fmt.Sprintf(`"%s" -mode run`, exe)
	if _, err := runReg("add", runKey, "/v", runValue, "/t", "REG_SZ", "/d", cmdline, "/f"); err != nil {
		return fmt.Errorf("写入开机自启失败（可能被安全软件拦截）: %w", err)
	}
	return nil
}

// autostartDisable 没装也当成功，方便重复执行。
func autostartDisable() error {
	runReg("delete", runKey, "/v", runValue, "/f")
	return nil
}

// autostartQuery 返回已登记的命令行；没装返回 ok=false。
func autostartQuery() (cmdline string, ok bool) {
	out, err := runReg("query", runKey, "/v", runValue)
	if err != nil {
		return "", false
	}
	if idx := strings.Index(out, "REG_SZ"); idx >= 0 {
		return strings.TrimSpace(out[idx+len("REG_SZ"):]), true
	}
	return "", false
}
