package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LoginResult 一次登录尝试的结果。
type LoginResult struct {
	OK               bool
	Message          string
	UserIndex        string
	KeepaliveSeconds int
	Raw              string
}

// PortalClient 维护一次认证会话：cookie、公钥、以及登录所需的全部参数。
type PortalClient struct {
	client    *http.Client
	base      string   // http://host/eportal/InterFace.do?method=
	bases     []string // 备选基地址，兼容不同厂商的部署路径
	pageURL   string   // http://host/eportal/index.jsp?<rawQuery>
	rawQuery  string
	mac       string
	service   string
	encrypt   bool
	exponent  *big.Int
	modulus   *big.Int
	modHexLen int
}

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{Transport: tr, Jar: jar, Timeout: 20 * time.Second}
}

// NewPortalClient 依据探针拿到的门户参数构造客户端。
func NewPortalClient(p *PortalParams, service string) *PortalClient {
	base := apiBase(p.PortalBase)
	page := p.PortalBase
	if p.RawQuery != "" {
		page += "?" + p.RawQuery
	}
	mac := p.Mac
	if mac == "" {
		mac = "111111111" // 与门户 JS 的兜底值一致
	}
	return &PortalClient{
		client:   newHTTPClient(),
		base:     base,
		bases:    apiBaseAlternates(p.PortalBase),
		pageURL:  page,
		rawQuery: p.RawQuery,
		mac:      mac,
		service:  service,
	}
}

// apiBase 从 .../eportal/index.jsp 推出 .../eportal/InterFace.do?method=
func apiBase(portalBase string) string {
	u, err := url.Parse(portalBase)
	if err != nil {
		return strings.Replace(portalBase, "/index.jsp", "/InterFace.do?method=", 1)
	}
	path := u.Path
	if i := strings.LastIndex(path, "/eportal/"); i >= 0 {
		u.Path = path[:i] + "/eportal/InterFace.do"
	} else {
		u.Path = "/eportal/InterFace.do"
	}
	u.RawQuery = ""
	return u.Scheme + "://" + u.Host + u.Path + "?method="
}

// apiBaseAlternates 给不同厂商的部署方式留后路：
// 有的门户 interface 在 /eportal/ 下，有的直接在根路径。
func apiBaseAlternates(portalBase string) []string {
	primary := apiBase(portalBase)
	u, err := url.Parse(portalBase)
	if err != nil {
		return []string{primary}
	}
	var alt string
	if strings.Contains(u.Path, "/eportal/") {
		alt = u.Scheme + "://" + u.Host + "/InterFace.do?method="
	} else {
		alt = u.Scheme + "://" + u.Host + "/eportal/InterFace.do?method="
	}
	if alt == primary {
		return []string{primary}
	}
	return []string{primary, alt}
}

// extractJSON 容忍 JSONP 包装：var xx = {...};
func extractJSON(raw []byte) []byte {
	s := string(raw)
	l := strings.Index(s, "{")
	r := strings.LastIndex(s, "}")
	if l >= 0 && r > l {
		return []byte(s[l : r+1])
	}
	return raw
}

func (c *PortalClient) do(ctx context.Context, method, target string, body []byte, contentType string) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if method == http.MethodPost {
		if contentType == "" {
			contentType = "application/x-www-form-urlencoded; charset=UTF-8"
		}
		req.Header.Set("Content-Type", contentType)
	}
	if c.pageURL != "" {
		req.Header.Set("Referer", c.pageURL)
	}
	if u, err := url.Parse(c.pageURL); err == nil {
		req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return data, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return data, nil
}

// Init 打开门户页拿到会话 cookie，再取 pageInfo 下发的公钥。
func (c *PortalClient) Init(ctx context.Context) error {
	if _, err := c.do(ctx, http.MethodGet, c.pageURL, nil, ""); err != nil {
		return fmt.Errorf("打开门户页失败: %w", err)
	}

	bases := c.bases
	if len(bases) == 0 {
		bases = []string{c.base}
	}
	var lastErr error
	for _, base := range bases {
		raw, err := c.do(ctx, http.MethodPost, base+"pageInfo",
			[]byte("queryString="+doubleURI(c.rawQuery)), "")
		if err != nil {
			lastErr = fmt.Errorf("pageInfo(%s) 失败: %w", base, err)
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(extractJSON(raw), &m); err != nil {
			lastErr = fmt.Errorf("pageInfo 不是 JSON: %w（前 120 字节: %.120s）", err, string(raw))
			continue
		}
		c.base = base

		if getStr(m, "validCodeUrl") != "" {
			return errors.New("该门户当前要求图形验证码，自动登录不可用（需要人工干预一次）")
		}
		if getStr(m, "passwordEncrypt") == "true" {
			eHex := getStr(m, "publicKeyExponent")
			nHex := getStr(m, "publicKeyModulus")
			if eHex == "" || nHex == "" {
				return errors.New("pageInfo 未下发公钥（passwordEncrypt=true 但公钥为空）")
			}
			e, ok1 := new(big.Int).SetString(eHex, 16)
			n, ok2 := new(big.Int).SetString(nHex, 16)
			if !ok1 || !ok2 {
				return fmt.Errorf("公钥解析失败: e=%s n=%.20s...", eHex, nHex)
			}
			c.exponent, c.modulus, c.encrypt = e, n, true
			c.modHexLen = len(nHex)
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("pageInfo 没有可用地址")
	}
	return lastErr
}

// Login 按门户 JS 的字段顺序与编码方式提交认证。
func (c *PortalClient) Login(ctx context.Context, cred *Credential, lg *Logger) (LoginResult, error) {
	pwd := cred.Password
	if c.encrypt {
		// 与 login_bch.js 一致：密码 <150 字符时，先拼 ">mac"，再整体反转，再 RSA
		if len(pwd) < 150 {
			salted := pwd + ">" + c.mac
			ct, err := rsaEncryptJS(c.exponent, c.modulus, reverseString(salted))
			if err != nil {
				return LoginResult{}, fmt.Errorf("密码加密失败: %w", err)
			}
			pwd = ct
		}
	}
	encFlag := "false"
	if c.encrypt {
		encFlag = "true"
	}

	body := "userId=" + doubleURI(cred.User) +
		"&password=" + doubleURI(pwd) +
		"&service=" + doubleURI(c.service) +
		"&queryString=" + doubleURI(c.rawQuery) +
		"&operatorPwd=&operatorUserId=&validcode=&passwordEncrypt=" + doubleURI(encFlag)

	raw, err := c.do(ctx, http.MethodPost, c.base+"login", []byte(body), "")
	if err != nil && len(raw) == 0 {
		return LoginResult{}, fmt.Errorf("提交登录失败: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(extractJSON(raw), &m); err != nil {
		return LoginResult{OK: false, Message: "返回体无法解析", Raw: string(raw)}, nil
	}

	res := LoginResult{
		Message:   getStr(m, "message"),
		Raw:       string(raw),
		UserIndex: getStr(m, "userIndex"),
	}
	if getStr(m, "result") == "success" {
		res.OK = true
		if v := getStr(m, "keepaliveInterval"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				res.KeepaliveSeconds = n
			}
		}
		if res.KeepaliveSeconds == 0 {
			res.KeepaliveSeconds = 60
		}
	} else {
		if res.Message == "" {
			res.Message = "门户返回失败（未给出 message）"
		}
	}
	if lg != nil {
		if res.OK {
			lg.Info("登录成功 userIndex=%s keepalive=%ds", maskUser(res.UserIndex), res.KeepaliveSeconds)
		} else {
			lg.Warn("登录被拒: %s", res.Message)
		}
	}
	return res, nil
}

// Keepalive 走门户自己的保活接口，比"刷一个 204 页面"更接近浏览器行为。
func (c *PortalClient) Keepalive(ctx context.Context, userIndex string) (bool, error) {
	raw, err := c.do(ctx, http.MethodPost, c.base+"keepalive", []byte("userIndex="+uriComponent(userIndex)), "")
	if err != nil {
		return false, err
	}
	var m map[string]any
	if err := json.Unmarshal(extractJSON(raw), &m); err != nil {
		return false, fmt.Errorf("keepalive 返回体无法解析: %.120s", string(raw))
	}
	return getStr(m, "result") == "success", nil
}

func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

// uriComponent 等价于 JS 的 encodeURIComponent（Go 的 QueryEscape 在空格等细节上不同）。
func uriComponent(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 3)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case strings.IndexByte("-_.!~*'()", ch) >= 0:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func doubleURI(s string) string { return uriComponent(uriComponent(s)) }
