package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// State 是网络的三态，加上一个"说不清"。
type State int

const (
	StateUnknown State = iota
	StateOnline
	StatePortal
	StateNoLink
)

func (s State) String() string {
	switch s {
	case StateOnline:
		return "ONLINE"
	case StatePortal:
		return "PORTAL"
	case StateNoLink:
		return "NO-LINK"
	default:
		return "UNKNOWN"
	}
}

// PortalParams 是从 302 Location 里解出来的门户参数。
// 注意：这些值在 URL 里是密文，本程序只做"原样搬运"，不做解密。
type PortalParams struct {
	RawQuery   string
	PortalBase string
	WlanUserIP string
	WlanACName string
	NasIP      string
	Mac        string
	NasID      string
	Vid        string
	Port       string
	NasPortID  string
	TargetURL  string
	T          string
}

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func probeClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil, // 关键：不吃系统代理
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		// 关键：不跟随跳转，把 302 本身当作"信号"看待
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Probe 探测一次网络状态。
func Probe(ctx context.Context, target string, timeout time.Duration, hostHint string) (State, *PortalParams, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return StateUnknown, nil, "构造请求失败: " + err.Error()
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "*/*")

	resp, err := probeClient(timeout).Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return StateNoLink, nil, "请求超时：可能 Wi-Fi 未关联或 DNS 未就绪"
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return StateNoLink, nil, "域名解析失败：可能未关联或不认证状态下 DNS 被拦"
		}
		return StateNoLink, nil, err.Error()
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	loc := resp.Header.Get("Location")

	switch {
	case resp.StatusCode == http.StatusNoContent:
		return StateOnline, nil, "204，且无跳转"

	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		if loc == "" {
			return StateUnknown, nil, fmt.Sprintf("HTTP %d 但没有 Location", resp.StatusCode)
		}
		u, perr := url.Parse(loc)
		if perr != nil {
			return StateUnknown, nil, "Location 解析失败: " + perr.Error()
		}
		if !u.IsAbs() {
			u = resp.Request.URL.ResolveReference(u)
		}
		if !looksLikePortal(loc, hostHint) {
			// 跳到正常站点（比如 baidu 自己 http->https），说明真实流量已经出去了
			return StateOnline, nil, fmt.Sprintf("HTTP %d 跳转至非门户 %s", resp.StatusCode, u.Host)
		}
		p := parsePortalParams(u)
		return StatePortal, p, fmt.Sprintf("HTTP %d 跳转至门户 %s", resp.StatusCode, u.Host)

	case resp.StatusCode == http.StatusOK:
		text := string(body)
		if strings.Contains(text, "wlanuserip") || strings.Contains(text, "/eportal/") {
			// 有些门户不 302，直接把登录页以 200 吐出来（"就地弹页"）。
			// 这时页面里通常还埋着带 wlanuserip 的门户地址，捞出来照样能登录。
			if p := findPortalParamsInBody(body, hostHint); p != nil {
				return StatePortal, p, "200 门户页（就地弹页），已从页面里解出参数"
			}
			// 页面里没有明文地址（参数在服务端 session 里），改从门户根路径取
			if p := fetchParamsFromPortalHost(hostHint, timeout); p != nil {
				return StatePortal, p, "200 门户页（就地弹页），已从门户根路径取到参数"
			}
			return StatePortal, nil, "200 但返回的是门户页（就地弹页，无 302，且取不到参数）"
		}
		return StateOnline, nil, "200，无跳转"

	default:
		return StateUnknown, nil, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
}

func looksLikePortal(loc, hint string) bool {
	low := strings.ToLower(loc)
	// Dr.COM ePortal 的强信号：带了 wlanuserip / wlanacname，或路径里有 InterFace.do
	for _, k := range []string{"eportal", "portal", "wlanuserip=", "wlanacname=", "interface.do"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	if hint != "" && strings.Contains(low, strings.ToLower(hint)) {
		return true
	}
	return false
}

func parsePortalParams(u *url.URL) *PortalParams {
	q := u.Query()
	return &PortalParams{
		RawQuery:   u.RawQuery,
		PortalBase: u.Scheme + "://" + u.Host + u.Path,
		WlanUserIP: q.Get("wlanuserip"),
		WlanACName: q.Get("wlanacname"),
		NasIP:      q.Get("nasip"),
		Mac:        q.Get("mac"),
		NasID:      q.Get("nasid"),
		Vid:        q.Get("vid"),
		Port:       q.Get("port"),
		NasPortID:  q.Get("nasportid"),
		TargetURL:  q.Get("url"),
		T:          q.Get("t"),
	}
}

// Summary 打印时用，只显示是否解析到关键字段，避免把密文刷屏。
func (p *PortalParams) Summary() string {
	if p == nil {
		return "未取得参数"
	}
	got := 0
	for _, v := range []string{p.WlanUserIP, p.WlanACName, p.NasIP, p.Mac, p.NasID, p.TargetURL} {
		if v != "" {
			got++
		}
	}
	return fmt.Sprintf("已解析 %d 个字段（wlanuserip=%t, wlanacname=%t, nasip=%t, mac=%t, nasid=%t, url=%t）",
		got,
		p.WlanUserIP != "", p.WlanACName != "", p.NasIP != "", p.Mac != "", p.NasID != "", p.TargetURL != "")
}

// ---------------------------------------------------------------- 就地弹页的兜底

var (
	absURLRe = regexp.MustCompile(`http[s]?://[^\s"'<>\\]+`)
	relURLRe = regexp.MustCompile(`/eportal/[^\s"'<>\\]*index\.jsp\?[^\s"'<>\\]+`)
)

// findPortalParamsInBody 从门户页 HTML 里捞带 wlanuserip 的门户地址。
// 支持绝对 URL、相对路径（配合已知门户主机）、以及被 HTML 转义过的 &amp;。
func findPortalParamsInBody(body []byte, hostHint string) *PortalParams {
	text := strings.ReplaceAll(string(body), "&amp;", "&")

	for _, raw := range absURLRe.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ");,.'")
		if !strings.Contains(raw, "wlanuserip=") {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		if p := parsePortalParams(u); p.WlanUserIP != "" {
			return p
		}
	}

	base := hostBase(hostHint)
	if base == "" {
		return nil
	}
	for _, raw := range relURLRe.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ");,.'")
		u, err := url.Parse(base + raw)
		if err != nil {
			continue
		}
		if p := parsePortalParams(u); p.WlanUserIP != "" {
			return p
		}
	}
	return nil
}

// fetchParamsFromPortalHost 页面里没有明文地址时，直接问门户本身：
// 未认证时门户根路径一般会 302 到带 wlanuserip 的 index.jsp。
func fetchParamsFromPortalHost(hostHint string, timeout time.Duration) *PortalParams {
	base := hostBase(hostHint)
	if base == "" {
		return nil
	}
	for _, path := range []string{"/", "/eportal/index.jsp"} {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", browserUA)
		resp, err := probeClient(timeout).Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
		resp.Body.Close()

		if loc := resp.Header.Get("Location"); loc != "" && strings.Contains(loc, "wlanuserip=") {
			u, err := url.Parse(loc)
			if err == nil {
				if u.Host == "" {
					u = resp.Request.URL.ResolveReference(u)
				}
				if p := parsePortalParams(u); p.WlanUserIP != "" {
					return p
				}
			}
		}
		if p := findPortalParamsInBody(body, hostHint); p != nil {
			return p
		}
	}
	return nil
}

// hostBase 把配置里的门户主机规整成 http(s)://host 形式。
func hostBase(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	if strings.HasPrefix(hint, "http://") || strings.HasPrefix(hint, "https://") {
		u, err := url.Parse(hint)
		if err != nil || u.Host == "" {
			return ""
		}
		return u.Scheme + "://" + u.Host
	}
	return "http://" + strings.TrimRight(hint, "/")
}
