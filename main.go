package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// version 由构建时注入：-ldflags "-X main.version=1.0.0"
var version = "0.1.0-dev"

func main() {
	defer restoreConsoleCP()

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "CampusAutoLogin v%s —— 校园网自动重认证\n\n", version)
		fmt.Fprintf(os.Stderr, "用法: CampusAutoLogin [参数]\n\n")
		flag.PrintDefaults()
	}
	configFile := flag.String("config", "config.json", "配置文件路径（相对路径按 exe 所在目录解析）")
	mode := flag.String("mode", "", "运行模式：setup | run | once | login | status | stop | disable | install | uninstall | reset")
	ver := flag.Bool("version", false, "显示版本号")
	flag.Parse()

	if *ver {
		fmt.Println("CampusAutoLogin v" + version)
		return
	}

	dir, err := dataDir()
	if err != nil {
		fatal("定位程序目录失败: %v", err)
		return
	}
	configPath = absUnder(dir, *configFile)
	appDir = dir

	cfg, err := loadConfig(configPath)
	if err != nil {
		fatal("读取配置失败: %v", err)
		return
	}
	cfg.CredFile = absUnder(dir, cfg.CredFile)
	cfg.LogFile = absUnder(dir, cfg.LogFile)

	m := *mode
	chatty := false
	switch m {
	case "run":
		// 静默：开机自启走这条，绝不弹窗
	case "":
		// 不带参数：没凭据就进向导，有凭据就后台跑
		if _, err := loadCredential(cfg.CredFile); err != nil {
			chatty = ensureConsole(true)
			m = "setup"
		} else if instanceRunning() {
			// 已经在后台跑着：双击时什么都没发生最让人困惑，给个明确反馈
			notifyRunning(cfg)
			return
		} else {
			chatty = ensureConsole(false)
			m = "run"
		}
	default:
		chatty = ensureConsole(true)
	}

	switch m {
	case "setup":
		doSetup(&cfg, chatty)
	case "run":
		doRun(&cfg, chatty)
	case "once":
		doOnce(cfg, chatty)
	case "login":
		doLogin(&cfg, chatty)
	case "status":
		doStatus(cfg, chatty)
	case "stop":
		doStop(false)
	case "disable":
		doStop(true)
	case "dump":
		doDump(cfg, chatty)
	case "install":
		doInstall(cfg, chatty)
	case "uninstall":
		doUninstall(chatty)
	case "reset":
		doReset(cfg, chatty)
	default:
		fmt.Fprintf(os.Stderr, "未知模式 %q\n可用：setup | run | once | login | status | stop | disable | install | uninstall | reset\n", m)
		os.Exit(2)
	}
}

// instanceRunning 只在"问一下"——拿到锁立刻放掉，不占着。
func instanceRunning() bool {
	release, ok := singleInstance()
	if !ok {
		return true
	}
	release()
	return false
}

// notifyRunning 双击时如果已经在跑，给一个"能关掉它"的入口，而不是只报状态。
func notifyRunning(cfg Config) {
	state, _, detail := Probe(context.Background(), cfg.Probe.Target, 3*time.Second, cfg.Portal.HostHint)
	msg := fmt.Sprintf("程序已经在后台运行，正在帮你保持校园网登录，不需要再点一次。\n\n当前网络状态: %s\n%s\n\n想关掉它？点下面的按钮：\n\n【是】彻底不用了：立刻停止，以后开机也不再自动运行\n【否】只停这一次：立刻停止，下次开电脑还会自动运行\n【取消】什么都不做，继续后台运行", state, detail)
	if ensureConsole(false) {
		fmt.Println(msg)
		return
	}
	switch alertChoice("CampusAutoLogin 正在后台运行", msg) {
	case msgYes:
		doStop(true)
		alert("已停止", "CampusAutoLogin 已停止运行，开机自动启动也取消了。\n\n以后想再用：双击 CampusAutoLogin.exe，重新填一次账号密码就行。")
	case msgNo:
		doStop(false)
		alert("已停止", "CampusAutoLogin 已停止运行。\n\n下次开电脑会自动重新运行；现在想恢复，双击 CampusAutoLogin.exe 即可。")
	}
}

// doStop 让后台实例干净退出；disableAutostart 为真时连开机自启一起取消。
func doStop(disableAutostart bool) {
	running := instanceRunning()
	gone := true
	if running {
		signalStop()
		gone = processGone()
	}
	if !running {
		fmt.Println("当前没有在后台运行。")
	} else if gone {
		fmt.Println("后台实例已停止。")
	} else {
		fmt.Println("后台实例没有响应停止信号（可能正卡在网络请求上），请过几秒再试一次。")
	}
	if disableAutostart {
		if err := autostartDisable(); err != nil {
			fmt.Printf("取消开机自启失败: %v\n", err)
		} else {
			fmt.Println("已取消开机自动启动。")
			return
		}
	}
	if _, ok := autostartQuery(); ok {
		fmt.Println("开机自动启动仍然开着（下次开机还会运行）。")
	}
}

// fatal 有控制台就打印，没有就弹窗——避免双击了但什么都没发生。
func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if ensureConsole(false) {
		fmt.Fprintf(os.Stderr, "错误: %s\n", msg)
		return
	}
	alert("CampusAutoLogin 出错", msg)
}

func absUnder(dir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

// dataDir 优先用 exe 所在目录（绿色便携），不可写时退到 %LOCALAPPDATA%。
func dataDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	if dirWritable(dir) {
		return dir, nil
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = os.TempDir()
	}
	fallback := filepath.Join(local, "CampusAutoLogin")
	if err := os.MkdirAll(fallback, 0o755); err != nil {
		return "", err
	}
	return fallback, nil
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".wtest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// ---------------------------------------------------------------- setup

func doSetup(cfg *Config, chatty bool) {
	if !chatty {
		// 没有控制台就没法交互，只能提示
		alert("CampusAutoLogin", "首次使用需要设置账号密码，请双击运行程序（或从命令行加 -mode setup）。")
		return
	}

	fmt.Println()
	fmt.Println("  CampusAutoLogin v" + version)
	fmt.Println("  ============================")
	fmt.Println()
	fmt.Println("  账号密码只以 Windows DPAPI 加密存在这台电脑，")
	fmt.Println("  只有当前 Windows 用户能解开，不联网、不上传。")
	fmt.Println("  配置文件:", configPath)
	fmt.Println()

	user := ""
	for user == "" {
		fmt.Print("  账号（学号 / 工号）: ")
		user = strings.TrimSpace(readLine())
		if user == "" {
			fmt.Println("  ! 不能为空")
		}
	}

	pwd := ""
	for pwd == "" {
		pwd = readSecret("  密码（输入时不显示）: ")
		if pwd == "" {
			fmt.Println("  ! 不能为空")
		}
	}

	fmt.Println()
	fmt.Println("  运营商（不确定就按回车，程序会自动试）:")
	fmt.Println("    1) 校园网 / 其他      2) 移动      3) 电信      4) 联通")
	fmt.Print("  选择 [1]: ")
	switch strings.TrimSpace(readLine()) {
	case "2":
		cfg.Portal.Service = "移动"
	case "3":
		cfg.Portal.Service = "电信"
	case "4":
		cfg.Portal.Service = "联通"
	default:
		cfg.Portal.Service = ""
	}

	// 顺手记住当前连的无线名，掉线时用它帮你连回来
	ssid := currentWifiSSID()
	if ssid == "" {
		fmt.Println()
		fmt.Print("  校园 Wi-Fi 名称（连着校园网会自动识别，不确定就回车跳过）: ")
		ssid = strings.TrimSpace(readLine())
	}
	if ssid != "" {
		cfg.Wifi.SSID = ssid
		fmt.Printf("  无线: %s（检测到完全没网时会自动连它）\n", ssid)
	}
	if err := saveCredential(cfg.CredFile, Credential{User: user, Password: pwd}); err != nil {
		fatal("保存凭据失败: %v", err)
		return
	}
	if err := saveConfig(*cfg); err != nil {
		fmt.Printf("  ! 保存配置失败: %v\n", err)
	}

	fmt.Println()
	if askYesNo("  开机自动登录（登录 Windows 后静默启动）？[Y/n]: ", true) {
		if exe, err := os.Executable(); err == nil {
			if err := autostartEnable(exe); err != nil {
				fmt.Printf("  ! %v\n", err)
			} else {
				fmt.Println("  已加入开机自启（可在 -mode uninstall 里取消）")
			}
		}
	}

	fmt.Println()
	fmt.Println("  正在检测当前网络…")
	state, params, detail := Probe(context.Background(), cfg.Probe.Target,
		secDur(cfg.Probe.TimeoutSec, 8), cfg.Portal.HostHint)
	fmt.Printf("  状态: %s（%s）\n", state, detail)
	if params != nil {
		learnPortalHost(cfg, params, nil)
	}

	switch state {
	case StatePortal:
		if params == nil {
			fmt.Println("  已连到校园网但没解析到门户参数，先跳过试登录。")
			break
		}
		if askYesNo("  现在就试登录一次？[Y/n]: ", true) {
			pc := NewPortalClient(params, cfg.Portal.Service)
			if err := pc.Init(context.Background()); err != nil {
				fmt.Printf("  ! 门户初始化失败: %v\n", err)
				break
			}
			res, err := pc.Login(context.Background(), &Credential{User: user, Password: pwd}, nil)
			if err != nil {
				fmt.Printf("  ! 登录请求失败: %v\n", err)
				break
			}
			if res.OK {
				fmt.Printf("  登录成功，保活间隔 %ds。\n", res.KeepaliveSeconds)
			} else {
				fmt.Printf("  登录失败: %s\n", res.Message)
				fmt.Println("  可能是运营商选错了，或账号密码/计费状态有问题。")
			}
		}
	case StateOnline:
		fmt.Println("  当前已在线，下次掉线会自动登录。")
	case StateNoLink:
		fmt.Println("  现在没连上校园 Wi-Fi，等连上后会自动登录。")
	}

	fmt.Println()
	fmt.Println("  ----------------------------")
	fmt.Println("  设置完成，这个窗口可以关掉了。")
	fmt.Println("  改账号/改密码: CampusAutoLogin -mode reset")
	fmt.Println("  看日志:        CampusAutoLogin -mode status")
	fmt.Println("  取消自启:      CampusAutoLogin -mode uninstall")
	fmt.Println()
	fmt.Print("  按回车关闭…")
	readLine()
}

func doReset(cfg Config, chatty bool) {
	if err := os.Remove(cfg.CredFile); err != nil && !os.IsNotExist(err) {
		fmt.Printf("删除凭据失败: %v\n", err)
		return
	}
	fmt.Println("已清除保存的账号密码。")
	fmt.Println("下次运行程序会重新进入设置向导（-mode setup 可立即开始）。")
}

// ---------------------------------------------------------------- 单次检测

func doOnce(cfg Config, chatty bool) {
	lg := newLogger(cfg.LogFile)
	defer lg.Close()

	state, params, detail := Probe(context.Background(), cfg.Probe.Target,
		secDur(cfg.Probe.TimeoutSec, 8), cfg.Portal.HostHint)

	fmt.Printf("版本     : v%s\n", version)
	fmt.Printf("探针地址 : %s\n", cfg.Probe.Target)
	fmt.Printf("当前状态 : %s\n", state)
	fmt.Printf("判定依据 : %s\n", detail)
	if params != nil {
		fmt.Printf("门户参数 : %s\n", params.Summary())
		fmt.Printf("门户入口 : %s\n", params.PortalBase)
	}
	if _, err := loadCredential(cfg.CredFile); err != nil {
		fmt.Printf("凭据状态 : 未就绪（%v）\n", err)
	} else {
		fmt.Println("凭据状态 : 已加载")
	}
	if cmd, ok := autostartQuery(); ok {
		fmt.Println("开机自启 : 已开启")
		_ = cmd
	} else {
		fmt.Println("开机自启 : 未开启")
	}
	if cfg.Wifi.SSID != "" {
		cur := currentWifiSSID()
		switch cur {
		case cfg.Wifi.SSID:
			cur = "已连接"
		case "":
			cur = "当前未连接"
		default:
			cur = "当前连的是 " + cur
		}
		fmt.Printf("无线     : %s（%s）\n", cfg.Wifi.SSID, cur)
	} else {
		fmt.Println("无线     : 未配置（不会自动重连无线）")
	}
	lg.Info("单次探测 state=%s detail=%s", state, detail)
}

// doLogin 手动验证：只在未认证状态可用，会打印门户的原始返回。
func doLogin(cfg *Config, chatty bool) {
	lg := newLogger(cfg.LogFile)
	defer lg.Close()

	state, params, detail := Probe(context.Background(), cfg.Probe.Target,
		secDur(cfg.Probe.TimeoutSec, 8), cfg.Portal.HostHint)
	fmt.Printf("当前状态 : %s（%s）\n", state, detail)

	if state != StatePortal || params == nil {
		fmt.Println("只有在未认证（PORTAL）状态下才能拿到本次认证参数。")
		fmt.Println("先断开认证（或重启电脑），保持连着校园 Wi-Fi，再运行一次。")
		return
	}
	learnPortalHost(cfg, params, lg)

	cred, err := loadCredential(cfg.CredFile)
	if err != nil {
		fmt.Printf("凭据未就绪: %v\n先执行: CampusAutoLogin -mode setup\n", err)
		return
	}

	for _, svc := range serviceCandidates(cfg.Portal.Service) {
		pc := NewPortalClient(params, svc)
		fmt.Printf("正在打开门户页、获取公钥（运营商=%q）…\n", svc)
		if err := pc.Init(context.Background()); err != nil {
			fmt.Printf("初始化失败: %v\n", err)
			lg.Error("初始化失败: %v", err)
			return
		}
		if pc.encrypt {
			fmt.Printf("密码加密 : RSA（模长 %d bit）\n", pc.modHexLen*4)
		} else {
			fmt.Println("密码加密 : 关闭（提交明文，按门户要求编码）")
		}
		res, err := pc.Login(context.Background(), cred, lg)
		if err != nil {
			fmt.Printf("登录请求失败: %v\n", err)
			return
		}
		if res.OK {
			fmt.Printf("结果     : 成功   userIndex=%s   保活间隔=%ds  运营商=%q\n",
				maskUser(res.UserIndex), res.KeepaliveSeconds, svc)
			fmt.Printf("原始返回 : %.400s\n", res.Raw)
			if svc != cfg.Portal.Service {
				cfg.Portal.Service = svc
				_ = saveConfig(*cfg)
				fmt.Printf("已把运营商 %q 记进配置。\n", svc)
			}
			return
		}
		fmt.Printf("结果     : 失败（运营商=%q）  %s\n", svc, res.Message)
		if isCredProblem(res.Message) {
			fmt.Println("运行结束：这像是账号或密码的问题，不再尝试其它运营商。")
			return
		}
		fmt.Println("换下一个运营商再试…")
		time.Sleep(2 * time.Second)
	}
}

// ---------------------------------------------------------------- 常驻

func doRun(cfg *Config, chatty bool) {
	lg := newLogger(cfg.LogFile)
	defer lg.Close()

	release, ok := singleInstance()
	if !ok {
		lg.Info("已有实例在运行，本次退出")
		if chatty {
			fmt.Println("已有实例在运行。")
		}
		return
	}
	defer release()

	exe, _ := os.Executable()
	lg.Info("启动常驻 v%s exe=%s 探针=%s", version, exe, cfg.Probe.Target)
	if chatty {
		fmt.Printf("CampusAutoLogin v%s 常驻中（Ctrl+C 退出）\n", version)
		fmt.Printf("探针 %s  日志 %s\n", cfg.Probe.Target, cfg.LogFile)
	}

	// 另外一条退出通道：双击 exe 的弹窗、-mode stop、停止运行.cmd 都往这里发信号。
	watchStop(func() {
		lg.Info("收到停止请求，退出")
		restoreConsoleCP()
		os.Exit(0)
	})

	interval := secDur(cfg.Loop.IntervalSec, 10)
	burstInt := secDur(cfg.Loop.WakeBurstIntervalSec, 5)
	burstSpan := secDur(cfg.Loop.WakeBurstSec, 60)
	minGap := secDur(cfg.Loop.MinReloginGapSec, 30)
	backoffMax := secDur(cfg.Loop.BackoffMaxSec, 60)
	portalBurstInt := secDur(cfg.Loop.PortalBurstIntervalSec, 2)
	portalBurstSpan := secDur(cfg.Loop.PortalBurstSec, 20)

	cands := serviceCandidates(cfg.Portal.Service)
	svcIdx := 0

	prev := StateUnknown
	lastTick := time.Now()
	var burstUntil time.Time
	var portalBurstUntil time.Time
	var lastTry time.Time
	var nextTry time.Time
	failed := 0
	credWarned := false
	paramWarned := false
	wifiRetry := secDur(cfg.Wifi.RetrySec, 60)
	var lastWifiTry time.Time
	wifiWarned := false

	var pc *PortalClient
	var userIndex string
	kaEvery := 60 * time.Second
	lastKA := time.Now()
	var lastParamKey string

	for {
		state, params, detail := Probe(context.Background(), cfg.Probe.Target,
			secDur(cfg.Probe.TimeoutSec, 8), cfg.Portal.HostHint)
		now := time.Now()

		// 睡眠/休眠回来时下一轮会明显迟到，用这个识别"刚被唤醒"
		if now.Sub(lastTick) > 2*interval {
			burstUntil = now.Add(burstSpan)
			lg.Info("检测到休眠恢复，接下来 %d 秒加密探测", cfg.Loop.WakeBurstSec)
		}
		lastTick = now

		if state != prev {
			lg.Info("状态变化 %s -> %s（%s）", prev, state, detail)
			if state == StatePortal && prev != StatePortal {
				// 刚掉认证：进入一段快速重试窗口，让恢复过程短到看不见
				portalBurstUntil = now.Add(portalBurstSpan)
				lg.Info("进入快速重试：每 %s 探测并尝试登录，持续 %s",
					portalBurstInt, portalBurstSpan)
			}
			prev = state
		}

		// 门户参数变了（换了 IP/MAC 或重新连了一次 Wi-Fi），旧会话作废
		key := ""
		if params != nil {
			key = params.Mac + "|" + params.WlanUserIP + "|" + params.RawQuery
		}
		if key != lastParamKey {
			pc, userIndex = nil, ""
			lastParamKey = key
		}

		switch state {
		case StatePortal:
			if params == nil {
				if !paramWarned {
					lg.Warn("命中门户页但取不到认证参数（就地弹页且页面里没有门户地址），本次无法登录")
					paramWarned = true
				}
				break
			}
			paramWarned = false
			learnPortalHost(cfg, params, lg)

			cred, err := loadCredential(cfg.CredFile)
			if err != nil {
				if !credWarned {
					lg.Warn("没有可用凭据（%v）；只做检测，不尝试登录", err)
					credWarned = true
				}
				break
			}
			credWarned = false

			gate := minGap
			if now.Before(portalBurstUntil) {
				gate = portalBurstInt
			}
			if now.Sub(lastTry) < gate || now.Before(nextTry) {
				break
			}
			lastTry = now

			if pc == nil {
				pc = NewPortalClient(params, cands[svcIdx])
				if err := pc.Init(context.Background()); err != nil {
					failed++
					nextTry = now.Add(failedDelay(failed, now.Before(portalBurstUntil), portalBurstInt, interval, backoffMax))
					lg.Error("门户初始化失败: %v", err)
					pc = nil
					break
				}
			}

			res, err := pc.Login(context.Background(), cred, lg)
			switch {
			case err != nil:
				failed++
				nextTry = now.Add(failedDelay(failed, now.Before(portalBurstUntil), portalBurstInt, interval, backoffMax))
				lg.Error("登录未完成: %v", err)
			case res.OK:
				failed = 0
				nextTry = time.Time{}
				userIndex = res.UserIndex
				kaEvery = time.Duration(res.KeepaliveSeconds) * time.Second
				lastKA = now
				// 登录成功不代表立刻能上网，继续密集探测几轮确认
				portalBurstUntil = now.Add(portalBurstSpan)
				lg.Info("登录成功，进入保活（每 %ds，运营商=%q）", res.KeepaliveSeconds, cands[svcIdx])
				if cands[svcIdx] != cfg.Portal.Service {
					cfg.Portal.Service = cands[svcIdx]
					if err := saveConfig(*cfg); err != nil {
						lg.Warn("把运营商记进配置失败: %v", err)
					} else {
						lg.Info("已记住可用运营商 %q", cands[svcIdx])
					}
				}
			default:
				failed++
				nextTry = now.Add(failedDelay(failed, now.Before(portalBurstUntil), portalBurstInt, interval, backoffMax))
				lg.Error("登录被拒（运营商=%q）: %s", cands[svcIdx], res.Message)
				switch {
				case isCredProblem(res.Message):
					// 密码/账号有问题时绝不能 2 秒一次地撞，立刻退出快速窗口
					portalBurstUntil = time.Time{}
					lg.Error("这像是账号或密码的问题，停止快速重试，也不再尝试其它运营商")
					svcIdx = len(cands) - 1
				case svcIdx+1 < len(cands):
					svcIdx++
					pc = nil
					lg.Info("换一个运营商重试: %q", cands[svcIdx])
				}
			}

		case StateOnline:
			failed = 0
			nextTry = time.Time{}
			if pc != nil && userIndex != "" && now.Sub(lastKA) >= kaEvery {
				lastKA = now
				ok, err := pc.Keepalive(context.Background(), userIndex)
				switch {
				case err != nil:
					lg.Warn("保活请求异常: %v", err)
				case !ok:
					lg.Warn("保活返回失败，会话可能已失效")
					pc, userIndex = nil, ""
				}
			}

		case StateNoLink:
			// 无线都没连上，登录一定失败。配了 SSID 就顺手叫 Windows 连回去。
			pc, userIndex = nil, ""
			if cfg.Wifi.SSID != "" && now.Sub(lastWifiTry) >= wifiRetry {
				lastWifiTry = now
				if err := connectWifi(cfg.Wifi.SSID); err != nil {
					if !wifiWarned {
						lg.Warn("重连无线 %q 失败: %v", cfg.Wifi.SSID, err)
						wifiWarned = true
					}
				} else {
					lg.Info("已请求 Windows 重连无线 %q", cfg.Wifi.SSID)
					wifiWarned = false
				}
			}
		}

		time.Sleep(nextWait(state, now, burstUntil, portalBurstUntil, interval, portalBurstInt, burstInt))
	}
}

// nextWait 决定这一轮睡多久。优先级：掉认证的快速窗口 > 唤醒后的加密探测 > 常态。
func nextWait(state State, now, wakeUntil, portalUntil time.Time, interval, portalInt, wakeInt time.Duration) time.Duration {
	if state == StatePortal && now.Before(portalUntil) {
		return portalInt
	}
	if now.Before(wakeUntil) {
		return wakeInt
	}
	return interval
}

// failedDelay 登录失败后等多久再试。快速窗口内只等 1~3 个间隔，窗口外走指数退避。
func failedDelay(failed int, inBurst bool, portalInt, base, max time.Duration) time.Duration {
	if !inBurst {
		return backoff(failed, base, max)
	}
	d := portalInt * time.Duration(failed)
	if d > 3*portalInt {
		d = 3 * portalInt
	}
	if d < portalInt {
		d = portalInt
	}
	return d
}

func learnPortalHost(cfg *Config, p *PortalParams, lg *Logger) {
	if p == nil || cfg.Portal.HostHint != "" {
		return
	}
	u, err := url.Parse(p.PortalBase)
	if err != nil || u.Host == "" {
		return
	}
	cfg.Portal.HostHint = u.Host
	if err := saveConfig(*cfg); err != nil {
		if lg != nil {
			lg.Warn("记住门户主机失败: %v", err)
		}
		return
	}
	if lg != nil {
		lg.Info("记住门户主机 %s", u.Host)
	}
}

// serviceCandidates 决定尝试运营商的顺序：配置值优先，然后常见三种，最后空值。
func serviceCandidates(current string) []string {
	base := []string{current, "移动", "电信", "联通", ""}
	out := make([]string, 0, len(base))
	seen := map[string]bool{}
	for _, s := range base {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// isCredProblem 用门户返回的 message 判断"再换运营商也没用"。
func isCredProblem(msg string) bool {
	if msg == "" {
		return false
	}
	for _, k := range []string{"密码", "帐号", "账号", "用户名", "不存在", "无效", "停机", "欠费", "余额"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- 安装 / 状态

func doInstall(cfg Config, chatty bool) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("取 exe 路径失败: %v\n", err)
		return
	}
	if err := autostartEnable(exe); err != nil {
		fmt.Printf("安装失败: %v\n", err)
		return
	}
	fmt.Println("已加入开机自启（当前用户）：")
	if cmd, ok := autostartQuery(); ok {
		fmt.Println("  " + cmd)
	}
	fmt.Println()
	fmt.Println("取消: CampusAutoLogin -mode uninstall")
	fmt.Println("说明: 登录 Windows 后会静默启动，不会弹命令行窗口。")
}

func doUninstall(chatty bool) {
	if err := autostartDisable(); err != nil {
		fmt.Printf("取消开机自启失败: %v\n", err)
		return
	}
	fmt.Println("已取消开机自启。凭据文件保留，可用 -mode reset 清除。")
}

func doStatus(cfg Config, chatty bool) {
	fmt.Printf("版本     : v%s\n", version)
	fmt.Printf("配置文件 : %s\n", configPath)
	if _, err := loadCredential(cfg.CredFile); err != nil {
		fmt.Printf("凭据     : 未就绪（%v）\n", err)
	} else {
		fmt.Println("凭据     : 已保存")
	}
	if cmd, ok := autostartQuery(); ok {
		fmt.Println("开机自启 : 已开启")
		fmt.Println("           " + cmd)
	} else {
		fmt.Println("开机自启 : 未开启")
	}
	if cfg.Wifi.SSID != "" {
		cur := currentWifiSSID()
		switch cur {
		case cfg.Wifi.SSID:
			cur = "已连接"
		case "":
			cur = "当前未连接"
		default:
			cur = "当前连的是 " + cur
		}
		fmt.Printf("无线     : %s（%s）\n", cfg.Wifi.SSID, cur)
	} else {
		fmt.Println("无线     : 未配置（不会自动重连无线）")
	}

	b, err := os.ReadFile(cfg.LogFile)
	if err != nil {
		fmt.Printf("日志     : 还没有（%v）\n", err)
		return
	}
	fmt.Printf("日志     : %s（最近 40 行）\n\n", cfg.LogFile)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	start := 0
	if len(lines) > 40 {
		start = len(lines) - 40
	}
	for _, ln := range lines[start:] {
		fmt.Println(ln)
	}
}

func backoff(failed int, base, max time.Duration) time.Duration {
	if failed <= 0 {
		return 0
	}
	d := base
	for i := 1; i < failed && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

func secDur(v, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Second
}

func maskUser(u string) string {
	if len(u) <= 3 {
		return u + "***"
	}
	return u[:3] + strings.Repeat("*", len(u)-3)
}

// ---------------------------------------------------------------- 诊断

// doDump 把探针这一跳的原始响应存盘，用于排查"就地弹页"这类拿不到参数的情况。
func doDump(cfg Config, chatty bool) {
	req, err := http.NewRequest(http.MethodGet, cfg.Probe.Target, nil)
	if err != nil {
		fmt.Printf("构造请求失败: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := probeClient(secDur(cfg.Probe.TimeoutSec, 8)).Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))

	base := "inline-probe-" + time.Now().Format("150405")
	htmlPath := filepath.Join(appDir, base+".html")
	txtPath := filepath.Join(appDir, base+".txt")

	var sb strings.Builder
	fmt.Fprintf(&sb, "探针地址: %s\n状态码  : %d\n", cfg.Probe.Target, resp.StatusCode)
	for k, v := range resp.Header {
		fmt.Fprintf(&sb, "%s: %s\n", k, strings.Join(v, " | "))
	}
	fmt.Fprintf(&sb, "正文长度: %d 字节\n", len(body))
	_ = os.WriteFile(txtPath, []byte(sb.String()), 0o600)
	_ = os.WriteFile(htmlPath, body, 0o600)

	fmt.Printf("状态码 %d，正文 %d 字节\n", resp.StatusCode, len(body))
	fmt.Println("已保存：")
	fmt.Println("  " + htmlPath)
	fmt.Println("  " + txtPath)
	fmt.Println("把这两个文件发给开发者就能定位为什么没登录。")
}
