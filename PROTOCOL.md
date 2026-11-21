# 协议笔记

记录本机实测到的门户认证流程（2026-11 抓包 + 逆向 JS + 代码复刻验证）。
只描述"如何复刻我自己账号的正常登录"，不涉及任何绕过。

## 0. 识别

| 项 | 值 |
|---|---|
| 产品 | 城市热点 Dr.COM ePortal（`eportal/index.jsp` + `InterFace.do` 接口风格） |
| 门户主机 | `10.20.30.40`（HTTP，非 HTTPS） |
| 静态资源 | `10.20.30.40:8080/eportal/interface/...` |
| 认证方式 | 账号 + 密码（RSA 加密），无图形验证码、无短信二次验证 |
| 运营商 | `net_access_type = 移动` |

## 1. 未认证时的重定向

访问任意未被放行的 HTTP 页面，会 302 到：

```
http://10.20.30.40/eportal/index.jsp?wlanuserip=<32hex>&wlanacname=<16hex>&ssid=
&nasip=<32hex>&snmpagentip=&mac=<32hex>&t=wireless-v2&url=<48hex>&apmac=
&nasid=<16hex>&vid=<16hex>&port=<16hex>&nasportid=<64hex>
```

- 这些值都是**密文**，程序只做原样搬运，不解密。
- 每次掉线重新生成，绑定当次 IP / MAC / AC / 时间。
- 该原始查询串（不含 `?`）后续要作为 `queryString` 参数提交，且**双次 URL 编码**。

## 2. 建立会话

`GET` 门户页 `index.jsp?<原始查询串>`，服务端下发 `JSESSIONID`。
后续所有 `InterFace.do` 请求都要带上这个 Cookie。

## 3. 取公钥（pageInfo）

```
POST http://10.20.30.40/eportal/InterFace.do?method=pageInfo
Content-Type: application/x-www-form-urlencoded; charset=UTF-8

queryString=<原始查询串，双次 URL 编码>
```

返回 JSON 关键字段（实测）：

| 字段 | 实测值 | 说明 |
|---|---|---|
| `passwordEncrypt` | `true` | 密码需要加密 |
| `publicKeyExponent` | `10001` | RSA 公钥指数 |
| `publicKeyModulus` | 256 hex 字符 | 1024 位模数 |
| `validCodeUrl` | 空 | 无需图形验证码 |
| `isCheckSmsAuth` | `false` | 无短信验证 |

## 4. 密码的加密方式

复刻自门户的 `AuthInterFace.js` / `security.js` / `login_bch.js`：

1. 若密码长度 < 150：先拼成 `密码 + ">" + mac`（`mac` 取第 1 步查询串里的 `mac`，为空则用 `111111111`）；
2. 把整个字符串**按字符反转**；
3. 无填充 RSA：把字节按**小端**顺序塞进大整数，每块长度 = 模数字节数 − 2（1024 位 → 126 字节），不足补 `0x00`；
4. 每块算 `m^e mod n`，输出补零到 `4*digits` 位十六进制（模数 256 hex → 输出 256 hex）。

第 3 步是这套 JS 特别的地方：**没有 PKCS#1 填充**，而且字节序是小端。`rsa.go` 与 `rsa_test.go` 用真 JS 做了逐字节交叉验证。

## 5. 登录

```
POST http://10.20.30.40/eportal/InterFace.do?method=login
Content-Type: application/x-www-form-urlencoded; charset=UTF-8

userId=<学号，双次 URL 编码>
&password=<第 4 步的 hex，或报文密码>
&service=<运营商，双次 URL 编码，如 移动>
&queryString=<原始查询串，双次 URL 编码>
&operatorPwd=&operatorUserId=&validcode=
&passwordEncrypt=true
```

返回 JSON：

- 成功：`"result":"success"`，并带 `userIndex`、`keepaliveInterval`
- 失败：`"result":"fail"`（或其它非 success 值），带 `message`，例如密码错误

## 6. 保活

```
POST http://10.20.30.40/eportal/InterFace.do?method=keepalive
Content-Type: application/x-www-form-urlencoded; charset=UTF-8

userIndex=<登录返回的 userIndex>
```

返回 `"result":"success"` 表示会话仍在。间隔取登录返回的 `keepaliveInterval`（默认 60 秒）。
这比"定时刷一个 204 页面"更贴近浏览器行为，也更不容易被网关判成异常。

## 7. 实测结论

- `pageInfo` 实测返回 `passwordEncrypt=true`、指数 `10001`、模数 1024 位、无验证码；
- RSA 实现与门户 JS 输出完全一致（`TestRSAEncryptMatchesJS`）；
- 抓包里的密码字段是 256 位十六进制密文，不是明文，脱离服务器私钥无法还原。
