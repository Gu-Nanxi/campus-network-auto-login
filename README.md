# CampusAutoLogin · 校园网自动登录 / 校园网自动认证

这是个 Windows 上的小工具，只有一个 exe 文件。它替你做一件事：**校园网掉线了，自动帮你重新登录**。

掉线、锁屏、休眠、重启、手动注销之后，一般都得自己再打开浏览器点一遍登录——这东西就是把这步自动化了。
适用于 Dr.COM ePortal（城市热点）系门户：校园网、宿舍网、单位园区网。

**关键词**：校园网自动登录、校园网自动认证、校园网掉线重连、校园网免认证登录、Dr.COM、城市热点、ePortal、captive portal（连上 Wi-Fi 没登录时会弹登录页的那种网络）、自动重连

- 只做两件事：把账号密码存在本机（Windows 自己加密）+ 掉线后自动重新登录、定期续期
- 不做：绕过认证、共享账号、多设备蹭网
- 一个 exe，Windows 10/11 64 位，不用安装，不用装运行库

已经在一套真实的校园门户上跑通整条流程：被弹登录页 → 从页面里取认证参数 → RSA 加密登录 → 定时续期 → 恢复上网。

## 下载哪个文件

发布页（Releases）里有两个 zip，**绝大多数人下 `-windows-amd64.zip`**：

| 你的电脑 | 下哪个 |
|---|---|
| Intel / AMD 处理器 —— 几乎所有笔记本、台式机（i3/i5/i7、锐龙都算） | **`CampusAutoLogin-v…-windows-amd64.zip`**（99% 的人是这个） |
| ARM 处理器 —— 骁龙 X 系列笔记本、Surface Pro X 一类 | `CampusAutoLogin-v…-windows-arm64.zip` |

**不确定自己是什么架构**：设置 → 系统 → 关于 → 看「系统类型」。
写 `基于 x64 的处理器` 就下 **amd64**；写 `ARM 架构` 才下 arm64。

下错了不会有任何损坏，双击时只会提示「无法在你的电脑上运行」，删掉换另一个就行。

> 页面上另外两个 `Source code (zip / tar.gz)` 是源码快照，不是程序，**不要下**。

---

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

1. 按上面「下载哪个文件」选对一个压缩包（拿不准就用 amd64）。解压出来是一个**名叫 `CampusAutoLogin-v…` 的文件夹**，把它整个放到桌面或 D 盘
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
这是这类工具的常态。请用 `-mode dump` 抓一份现场文件，再提 Issue。

---

## 自己编译（可选）

需要装 Go 1.21 或更新版本。

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o CampusAutoLogin.exe .
```

`-H windowsgui` 的作用是编译成“窗口程序”而不是“控制台程序”：双击、开机自启都不会闪黑框。程序需要打印内容时，会自己去接管或新建一个控制台。

```powershell
go test ./...
```

`TestRSAEncryptMatchesJS` 会拿门户真实下发的那份 `security.js` 在 node 里跑一遍，和 Go 里的加密结果逐字符比对。

## License

MIT

<details>
<summary><b>English (for non-Chinese readers)</b></summary>

**CampusAutoLogin** — automatic re-login for Dr.COM ePortal (captive portal) campus networks on Windows.

**What it does**

- Stores your account once, locally, encrypted by Windows DPAPI. Nothing is uploaded.
- Watches the network and, when you are connected to Wi-Fi but not authenticated, logs in again. Covers Wi-Fi drops, screen lock, sleep, reboot and manual logout.
- Renews the session with the portal's own keepalive call instead of re-visiting pages.
- Optionally reconnects the Wi-Fi profile you configured, using `netsh wlan connect`.

**Download and run**

1. Get `CampusAutoLogin-v*-windows-amd64.zip` from Releases. Use `-arm64` only on ARM devices such as Snapdragon laptops.
2. Extract the archive. You get one folder. Put that folder on the Desktop or on `D:\`. Do not put it in `C:\Program Files`, which is not writable for normal users.
3. Double-click `CampusAutoLogin.exe`. Fill in account, password and carrier; press Enter if you are not sure about the carrier. Then choose whether to start with Windows.
4. That is all. It runs in the background with no window and no tray icon.

**How to stop it (no Task Manager needed)**

Double-click the exe again while it is running. A dialog offers three choices:

- Yes: stop now and also disable auto-start. It will not come back unless you run it again.
- No: stop just this time. It starts again after the next reboot.
- Cancel: keep it running.

**Compatibility**

- Dr.COM ePortal networks only: the login page is a captive portal, and its API uses `InterFace.do?method=login`. SANGFOR portals, Ruijie / Huawei / H3C 802.1X clients, and portals that require a captcha or SMS code are **not** supported.
- Windows 10 or 11, x64 or ARM64.
- The binary is unsigned. SmartScreen may warn: click "More info" then "Run anyway". You can also build it yourself.

**Build from source**

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o CampusAutoLogin.exe .
```

`-H windowsgui` builds a GUI-subsystem binary, so double-clicking or auto-starting never flashes a console window. The program attaches to or allocates a console by itself when it needs to print.

MIT licensed. Not affiliated with Dr.COM. It only automates the login you would do by hand; it does not bypass authentication or share accounts.

</details>
