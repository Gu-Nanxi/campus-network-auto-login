# CampusAutoLogin · 校园网自动登录 / 校园网自动认证

单文件的 Windows 小工具：**校园网掉线、息屏、休眠、重启之后，自动把认证补回去**。
面向 Dr.COM ePortal（城市热点）系门户，适用于校园网、宿舍网、单位园区网。

**关键词**：校园网自动登录 · 校园网自动认证 · 校园网掉线重连 · 校园网免认证登录 · Dr.COM · 城市热点 · ePortal · captive portal · 802.1X 门户 · 自动重连

> **English** — A single 7 MB Windows executable that automatically logs you back into a Dr.COM ePortal (captive portal) campus network after Wi-Fi drops, sleep, reboot, or manual logout, then keeps the session alive with periodic keepalive. Static Go build: no installer, no .NET, no runtime DLLs. Chinese UI, one-time account setup, silent afterwards.

- 只做两件事：记住你自己的账号密码（Windows DPAPI 本地加密）+ 掉线后自动重新登录、定期保活
- 不做：绕过认证、共享账号、多设备蹭网
- 一个 exe，Windows 10/11 64 位，无依赖、无安装包

曾在一套真实的城市热点 Dr.COM 门户上完整跑通：就地弹页 → 从页面取参 → RSA 登录 → 保活 → 恢复上网。

---

## 它怎么判断该不该登录

每 10 秒访问一次 `http://www.baidu.com/`（不跟随跳转），按结果分三种情况处理：

| 探测结果 | 含义 | 动作 |
|---|---|---|
| 正常响应 / 跳到正常网页 | 已经在线 | 什么都不做 |
| 跳到门户页，或就地返回登录页 | 掉认证了 | 取认证参数，提交登录 |
| 超时 / DNS 失败 | 无线都没连上 | 配了 Wi-Fi 名就叫 Windows 连回去 |

**掉认证后走快速重试**：检测到"连了网但没认证"，接下来 20 秒内每 2 秒探测一次并尝试登录
（失败就等 2→4→6 秒再试，不会一路退避到 60 秒），登录成功后也继续密集探测几轮确认真的通了。
所以掉线到恢复通常只差十几秒，而不是原来的半分钟以上。窗口结束后回到常态间隔。
密码错时这个窗口会立刻关掉，避免两秒一次地撞网关把账号锁掉。

登录成功后按门户返回的 `keepaliveInterval` 调保活接口续会话，而不是反复刷网页。

---

## 使用

1. 下载的是个压缩包，解压出来是一个**名叫 `CampusAutoLogin-v…` 的文件夹**，把它整个放到桌面或 D 盘
   （**别放 `C:\Program Files`**，那里普通用户写不进文件；也别只把 exe 单独拖出来，配置和日志会留在原地）
2. 双击文件夹里的 `CampusAutoLogin.exe`，在弹出的黑框里填账号、密码、运营商（不确定就回车），再选要不要开机自启
3. 之后就什么都不用管了

| 命令 | 作用 |
|---|---|
| `CampusAutoLogin.exe` | 没设过账号 → 进设置向导；设过且已在跑 → 弹窗问你要不要停止 |
| `-mode status` | 版本、凭据、自启、无线、最近 40 行日志 |
| `-mode once` | 只检测一次，打印它对你学校门户的判断 |
| `-mode login` | 手动试一次登录（要在"连了 Wi-Fi 但没认证"时用） |
| `-mode stop` | 停掉后台运行，下次开机还会自动跑 |
| `-mode disable` | 停掉后台运行，并取消开机自启（彻底不用） |
| `-mode reset` | 清除账号密码，下次重新向导 |
| `-mode install` / `-mode uninstall` | 装 / 卸开机自启 |
| `-mode dump` | 把探测到的原始页面存成文件，用于排查 |

---

## 不想用了怎么关（不用任务管理器）

在后台运行时它没有窗口，也看不见托盘图标，所以关法是**再双击一次 exe**，弹窗里三选一：

| 按钮 | 结果 |
|---|---|
| 是 | 彻底不用了：立刻停止，并且以后开机不再自动启动 |
| 否 | 只停这一次：立刻停止，下次开电脑还会自动跑 |
| 取消 | 什么都不做，继续后台运行 |

想再用的时候双击 exe 即可，不用重新填账号（账号密码还留着）。想连账号一起清掉，用 `-mode reset`。

---

## 无线断连这件事它只管一半

**认证**和**无线关联**是两码事。程序只能替你完成"门户登录"，Wi-Fi 自己断开后能不能连回来，取决于 Windows。

三件事建议做掉，掉线会少很多：

1. 校园 Wi-Fi 的属性里勾上「在信号范围内自动连接」；
2. 设备管理器 → 网络适配器 → 无线网卡 → 属性 → 电源管理 → 取消勾选「允许计算机关闭此设备以节约电源」（**息屏掉线多半是它**）；
3. 电源选项里把插电时的「睡眠」设为从不。

程序另外做了一层兜底：设置时会记住你当时连的无线名，一旦探测到"完全没网"，就每隔 60 秒叫 Windows 用 `netsh wlan connect` 连回去。对应 `config.json` 的 `wifi.ssid`，留空则不做。

---

## 能不能用在你学校

看**未登录**时随便打开一个网页会跳到哪：

| 跳转地址 | 结论 |
|---|---|
| `…/eportal/index.jsp?wlanuserip=…` | 能用（城市热点 Dr.COM 系） |
| `10.x.x.x/a79.htm` 之类 | 不能用（深信服） |
| 必须装客户端才能上网（锐捷/华为/H3C） | 不能用（802.1X，不是网页认证） |
| 要图形验证码或短信验证 | 不能用 |

拿不准就跑一次 `-mode once`，它会打印「判定依据 / 门户入口 / 门户参数」三行。

---

## 配置文件

首次运行在 exe 旁边生成 `config.json`；目录不可写时自动退到 `%LOCALAPPDATA%\CampusAutoLogin\`。常改的就几个：

| 字段 | 说明 |
|---|---|
| `probe.target` | 探测地址，必须是没被门户放行的域名，否则未认证也会显示在线 |
| `loop.interval_sec` | 在线时的探测间隔，默认 10 |
| `loop.portal_burst_sec` | 掉认证后快速重试持续多久，默认 20 |
| `loop.portal_burst_interval_sec` | 快速重试期间的探测间隔，默认 2 |
| `portal.host_hint` | 门户主机，留空则探测到自动记住 |
| `portal.service` | 运营商（移动/电信/联通），留空则自动逐个试 |
| `wifi.ssid` | 校园 Wi-Fi 名，留空则不帮你重连无线 |

---

## 常见问题

**杀毒软件 / SmartScreen 报风险**
无签名、会写注册表自启、常驻后台，启发式引擎容易误报。代码全在仓库里，可以自己编。不放心就别用。

**登录一直失败**
跑 `-mode login`，它会把门户返回的 `message` 原样打出来：密码错、运营商错、欠费停机的文案不一样。

**会不会被学校发现**
它就是你手动登录的自动化版本，请求特征和频率都接近浏览器。但**一个账号给多台设备用**是另一回事，属于违反校园网规定，本工具不提供。

**学校升级门户后失效**
正常宿命。带 `-mode dump` 的文件提 Issue。

---

## 从源码构建

需要 Go 1.21+。

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o CampusAutoLogin.exe .
```

`-H windowsgui` 让 exe 以 GUI 子系统启动，双击和自启都不带黑框；需要输出时程序会自己附着或新建控制台。

```powershell
go test ./...
```

`TestRSAEncryptMatchesJS` 会拿门户真实的 `security.js` 在 node 里跑，和 Go 实现逐字符比对。

## License

MIT

---

## English

**CampusAutoLogin** — auto re-login for Dr.COM ePortal (captive portal) campus Wi-Fi on Windows.

**What it does**

- Saves your account once (Windows DPAPI, local only, never uploaded)
- Detects "connected to Wi-Fi but not authenticated" and logs in again — after Wi-Fi drops, screen lock, sleep, reboot, or manual logout
- Keeps the session alive with periodic `keepalive` calls
- Optionally reconnects the Wi-Fi profile you configured (`netsh wlan connect`)

**Download & run**

1. Download `CampusAutoLogin-v*-windows-amd64.zip` from Releases (use `-arm64` only on ARM devices such as Snapdragon laptops)
2. Extract — you get one folder — and put that folder on Desktop or `D:\` (not `C:\Program Files`, which is not writable)
3. Double-click `CampusAutoLogin.exe`, fill in account / password / carrier (press Enter if unsure), choose whether to start with Windows
4. Done. It runs silently in the background, no window, no tray icon

**Stopping it (no Task Manager needed)**
Double-click the exe again while it is running, and pick one of three:

| Button | Result |
|---|---|
| Yes | Stop now **and** disable auto-start (never again unless you re-run it) |
| No | Stop just this time; it will come back after the next reboot |
| Cancel | Keep running |

**Compatibility**

- Dr.COM ePortal portals only (the kind whose login page says `InterFace.do?method=login`, or a portal URL behind a captive redirect). SANGFOR / Ruijie / Huawei 802.1X clients, or portals requiring a captcha or SMS code, are **not** supported.
- Windows 10 / 11, x64 or ARM64
- Unsigned binary: SmartScreen may warn → "More info" → "Run anyway". Build it yourself if you prefer.

**Build**

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o CampusAutoLogin.exe .
```

`-H windowsgui` makes it a GUI-subsystem binary: no console flash when double-clicked or auto-started. The program attaches to or allocates a console itself when it needs to print.

MIT licensed. Not affiliated with Dr.COM. It only automates the login you would otherwise do by hand — it does not bypass authentication or share accounts.
