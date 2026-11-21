//go:build windows

package main

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

// 注意：netsh 的界面文案随系统语言变化（英文 connected / 中文「已连接」），
// 所以状态判断两种都认；SSID 那一行用行首锚点，避免匹配到 "AP BSSID"。

var (
	reSSIDLine  = regexp.MustCompile(`(?m)^\s*SSID\s*:\s*(.+)$`)
	reStateLine = regexp.MustCompile(`(?m)^\s*(State|状态)\s*:\s*(.+)$`)
)

func netsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// currentWifiSSID 返回当前已关联的无线网络名；没连无线时返回空串。
func currentWifiSSID() string {
	out, err := netsh("wlan", "show", "interfaces")
	if err != nil {
		return ""
	}
	if !wifiStateConnected(out) {
		return ""
	}
	if m := reSSIDLine.FindStringSubmatch(out); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func wifiStateConnected(out string) bool {
	m := reStateLine.FindStringSubmatch(out)
	if len(m) != 3 {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(m[2]))
	return strings.Contains(s, "connected") || strings.Contains(s, "已连接")
}

// connectWifi 让 Windows 去连一个已保存的配置文件。本身不等待结果，
// 关联成功与否由下一轮探测的 NO-LINK/ONLINE 反映。
func connectWifi(ssid string) error {
	if strings.TrimSpace(ssid) == "" {
		return errors.New("Wi-Fi 名为空")
	}
	out, err := netsh("wlan", "connect", "name="+ssid, "ssid="+ssid)
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}
