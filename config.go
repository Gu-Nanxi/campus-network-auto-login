package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProbeConfig 连通性探针配置。
type ProbeConfig struct {
	// Target 必须是一个"未被门户白名单放行"的地址，
	// 否则未认证时也会返回 204，导致一直以为在线。
	Target     string `json:"target"`
	TimeoutSec int    `json:"timeout_sec"`
}

// LoopConfig 轮询与退避策略。
type LoopConfig struct {
	IntervalSec          int `json:"interval_sec"`
	WakeBurstSec         int `json:"wake_burst_sec"`
	WakeBurstIntervalSec int `json:"wake_burst_interval_sec"`
	MinReloginGapSec     int `json:"min_relogin_gap_sec"`
	BackoffMaxSec        int `json:"backoff_max_sec"`
	// 掉认证后的快速重试：这段时间里探测与登录都按 PortalBurstIntervalSec 来，
	// 登录一成功就基本"看不见"掉线过程；密码错时这扇窗会立刻关掉，避免撞网关。
	PortalBurstSec         int `json:"portal_burst_sec"`
	PortalBurstIntervalSec int `json:"portal_burst_interval_sec"`
}

// PortalConfig 门户侧配置。
type PortalConfig struct {
	// HostHint 是门户主机（如 10.20.30.40 或 auth.xxx.edu.cn）。
	// 留空时程序会在第一次探测到门户跳转时自动记住。
	HostHint string `json:"host_hint"`
	// Service 是运营商/网络类型，门户里叫 net_access_type，例如 移动 / 电信 / 联通。
	// 留空时程序会按 空 → 移动 → 电信 → 联通 的顺序自动试。
	Service string `json:"service"`
	Referer string `json:"referer"`
	Method  string `json:"method"`
}

type Config struct {
	Probe    ProbeConfig  `json:"probe"`
	Loop     LoopConfig   `json:"loop"`
	Portal   PortalConfig `json:"portal"`
	Wifi     WifiConfig   `json:"wifi"`
	CredFile string       `json:"cred_file"`
	LogFile  string       `json:"log_file"`
}

// WifiConfig 让程序在"无线没连上"时帮忙连回已保存的网络。
// 认证是门户的事，关联是 Windows 的事——这一步只是把后者也自动化。
type WifiConfig struct {
	// SSID 是校园 Wi-Fi 的名字；留空则不做任何无线操作。
	SSID     string `json:"ssid"`
	RetrySec int    `json:"retry_sec"`
}

// configPath 是本次实际使用的配置文件绝对路径（saveConfig 用它回写）。
var configPath string

// appDir 是数据目录（exe 所在目录，或不可写时的 LOCALAPPDATA 回退）。
var appDir string

// relToAppDir 把绝对路径还原成相对路径，保证 config.json 换目录后还能用。
func relToAppDir(p string) string {
	if appDir == "" || p == "" {
		return p
	}
	rel, err := filepath.Rel(appDir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}

func defaultConfig() Config {
	return Config{
		Probe: ProbeConfig{
			Target:     "http://www.baidu.com/",
			TimeoutSec: 8,
		},
		Loop: LoopConfig{
			IntervalSec:            10,
			WakeBurstSec:           60,
			WakeBurstIntervalSec:   5,
			MinReloginGapSec:       30,
			BackoffMaxSec:          60,
			PortalBurstSec:         20,
			PortalBurstIntervalSec: 2,
		},
		Portal: PortalConfig{
			Method: "POST",
		},
		Wifi: WifiConfig{
			RetrySec: 60,
		},
		CredFile: "cred.dat",
		LogFile:  "autologin.log",
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			out, mErr := json.MarshalIndent(cfg, "", "  ")
			if mErr != nil {
				return cfg, mErr
			}
			if wErr := os.WriteFile(path, out, 0o600); wErr != nil {
				return cfg, fmt.Errorf("写出默认配置失败: %w", wErr)
			}
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(b, &cfg); err != nil {
		// 记事本保存的 UTF-8 会带 BOM，去掉再试一次
		b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
		b = bytes.TrimPrefix(b, []byte("\uFEFF"))
		if err2 := json.Unmarshal(b, &cfg); err2 != nil {
			return cfg, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	if configPath == "" {
		return nil
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}
