package main

import (
	"net/url"
	"testing"
)

// 合成的门户 URL，结构与真实门户一致（参数值是密文），只用于验证解析不丢字段。
const samplePortalLocation = "http://10.20.30.40/eportal/index.jsp?wlanuserip=0123456789abcdef0123456789abcdef&wlanacname=aaaabbbbccccdddd&ssid=&nasip=11112222333344445555666677778888&snmpagentip=&mac=00112233445566778899aabbccddeeff&t=wireless-v2&url=abcdef0123456789abcdef0123456789abcdef0123456789&apmac=&nasid=aaaabbbbccccdddd&vid=0123456789abcdef&port=fedcba9876543210&nasportid=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParsePortalParams(t *testing.T) {
	u, err := url.Parse(samplePortalLocation)
	if err != nil {
		t.Fatalf("URL 解析失败: %v", err)
	}

	p := parsePortalParams(u)
	if p == nil {
		t.Fatal("parsePortalParams 返回 nil")
	}

	want := map[string]string{
		"wlanuserip": p.WlanUserIP,
		"wlanacname": p.WlanACName,
		"nasip":      p.NasIP,
		"mac":        p.Mac,
		"nasid":      p.NasID,
		"vid":        p.Vid,
		"port":       p.Port,
		"nasportid":  p.NasPortID,
		"url":        p.TargetURL,
		"t":          p.T,
	}
	for name, v := range want {
		if v == "" {
			t.Errorf("字段 %s 没有解析出来", name)
		}
	}

	if p.PortalBase != "http://10.20.30.40/eportal/index.jsp" {
		t.Errorf("PortalBase 不对: %s", p.PortalBase)
	}
	if p.WlanUserIP != "0123456789abcdef0123456789abcdef" {
		t.Errorf("wlanuserip 值被改动: %s", p.WlanUserIP)
	}
}

func TestLooksLikePortal(t *testing.T) {
	cases := []struct {
		loc  string
		hint string
		want bool
	}{
		{"http://10.20.30.40/eportal/index.jsp?wlanuserip=x", "10.20.30.40", true},
		{"http://10.0.0.8/eportal/index.jsp", "", true},
		{"https://www.baidu.com/", "10.20.30.40", false},
		{"https://www.baidu.com/s?wd=1", "10.20.30.40", false},
	}
	for _, c := range cases {
		if got := looksLikePortal(c.loc, c.hint); got != c.want {
			t.Errorf("looksLikePortal(%q, %q) = %v, want %v", c.loc, c.hint, got, c.want)
		}
	}
}

func TestFindPortalParamsInBody(t *testing.T) {
	html := []byte(`<html><script>window.location.href="http://10.20.30.40/eportal/index.jsp?wlanuserip=0123456789abcdef0123456789abcdef&wlanacname=aaaabbbbccccdddd&mac=00112233445566778899aabbccddeeff&amp;t=wireless-v2"</script></html>`)
	p := findPortalParamsInBody(html, "")
	if p == nil {
		t.Fatal("没有从 HTML 里解出门户参数")
	}
	if p.WlanUserIP != "0123456789abcdef0123456789abcdef" {
		t.Errorf("wlanuserip 不对: %s", p.WlanUserIP)
	}
	if p.Mac != "00112233445566778899aabbccddeeff" {
		t.Errorf("mac 没解开 &amp; 转义: %s", p.Mac)
	}

	// 相对路径 + 已知门户主机
	rel := []byte(`<a href="/eportal/index.jsp?wlanuserip=ffeeddccbbaa9988ffeeddccbbaa9988&mac=0011">go</a>`)
	if p := findPortalParamsInBody(rel, "10.20.30.40"); p == nil || p.WlanUserIP != "ffeeddccbbaa9988ffeeddccbbaa9988" {
		t.Error("相对路径没解析出来")
	}
	if p := findPortalParamsInBody(rel, ""); p != nil {
		t.Error("没有门户主机时不应该瞎猜")
	}

	// 普通网页不应该被误判
	if p := findPortalParamsInBody([]byte(`<html>hello</html>`), "10.20.30.40"); p != nil {
		t.Error("普通网页被误判成门户")
	}
}
