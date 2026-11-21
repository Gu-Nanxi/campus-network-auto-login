package main

import (
	"testing"
	"time"
)

func TestNextWaitPriority(t *testing.T) {
	now := time.Now()
	interval := 10 * time.Second
	portalInt := 2 * time.Second
	wakeInt := 5 * time.Second
	wakeUntil := now.Add(30 * time.Second)
	portalUntil := now.Add(20 * time.Second)

	cases := []struct {
		name   string
		state  State
		expect time.Duration
	}{
		// 掉认证且窗口没结束：2 秒，优先级最高
		{"portal-burst", StatePortal, portalInt},
		// 唤醒窗口内：5 秒
		{"wake-burst", StateOnline, wakeInt},
		// 唤醒中途掉认证：仍按 2 秒
		{"portal-during-wake", StatePortal, portalInt},
	}
	for _, c := range cases {
		if got := nextWait(c.state, now, wakeUntil, portalUntil, interval, portalInt, wakeInt); got != c.expect {
			t.Fatalf("%s: got %s want %s", c.name, got, c.expect)
		}
	}

	// 两个窗口都过期：回到常态间隔
	expired := now.Add(-time.Second)
	if got := nextWait(StatePortal, now, expired, expired, interval, portalInt, wakeInt); got != interval {
		t.Fatalf("expired portal burst: got %s want %s", got, interval)
	}
	if got := nextWait(StateOnline, now, expired, expired, interval, portalInt, wakeInt); got != interval {
		t.Fatalf("expired wake burst: got %s want %s", got, interval)
	}
}

func TestFailedDelay(t *testing.T) {
	portalInt := 2 * time.Second
	base := 10 * time.Second
	max := 60 * time.Second

	// 快速窗口内：2s / 4s / 6s，封顶 6s，不会一路涨到 60s
	for _, c := range []struct {
		failed int
		expect time.Duration
	}{{1, 2 * time.Second}, {2, 4 * time.Second}, {3, 6 * time.Second}, {9, 6 * time.Second}} {
		if got := failedDelay(c.failed, true, portalInt, base, max); got != c.expect {
			t.Fatalf("burst failed=%d: got %s want %s", c.failed, got, c.expect)
		}
	}

	// 窗口外：指数退避，封顶 backoffMax
	if got := failedDelay(1, false, portalInt, base, max); got != base {
		t.Fatalf("backoff failed=1: got %s want %s", got, base)
	}
	if got := failedDelay(10, false, portalInt, base, max); got != max {
		t.Fatalf("backoff failed=10: got %s want %s", got, max)
	}
	// 窗口内失败次数为 0 时不应返回 0，否则会变成死循环
	if got := failedDelay(0, true, portalInt, base, max); got != portalInt {
		t.Fatalf("burst failed=0: got %s want %s", got, portalInt)
	}
}
