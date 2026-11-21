package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRSAEncryptMatchesJS 用门户自己的 security.js 交叉验证 Go 的实现。
// 没有 node 或没有参考文件时自动跳过。
func TestRSAEncryptMatchesJS(t *testing.T) {
	jsPath, err := filepath.Abs(filepath.Join("reference", "security.js"))
	if err != nil || !fileExists(jsPath) {
		t.Skip("缺少 reference/security.js，跳过 JS 交叉验证")
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过 JS 交叉验证")
	}

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	e := big.NewInt(int64(key.PublicKey.E))
	n := key.PublicKey.N

	// 与门户一致的输入形态：密码 + ">" + mac，然后整体反转
	raw := "hunter2>00112233445566778899aabbccddeeff"
	input := reverseString(raw)

	got, err := rsaEncryptJS(e, n, input)
	if err != nil {
		t.Fatalf("Go 加密失败: %v", err)
	}

	script := `
const vm = require('vm'), fs = require('fs');
global.window = global;
var src = fs.readFileSync(process.argv[2]);
if (src.length >= 3 && src[0] === 0xEF && src[1] === 0xBB && src[2] === 0xBF) { src = src.slice(3); }
vm.runInThisContext(src.toString('utf8'));
var k = RSAUtils.getKeyPair(process.argv[3], '', process.argv[4]);
process.stdout.write(RSAUtils.encryptedString(k, process.argv[5]));
`
	tmp := filepath.Join(t.TempDir(), "cross.js")
	if err := os.WriteFile(tmp, []byte(script), 0o600); err != nil {
		t.Fatalf("写临时脚本失败: %v", err)
	}

	out, err := exec.Command(nodePath, tmp, jsPath, e.Text(16), n.Text(16), input).CombinedOutput()
	if err != nil {
		t.Fatalf("node 执行失败: %v\n%s", err, string(out))
	}
	want := strings.TrimSpace(string(out))

	if got != want {
		t.Fatalf("与 JS 结果不一致\n Go: %s\n JS: %s", truncate(got, 80), truncate(want, 80))
	}

	// 再用私钥还原，确认字节序确实是"小端"
	ct, _ := new(big.Int).SetString(strings.ReplaceAll(got, " ", ""), 16)
	m := new(big.Int).Exp(ct, key.D, n)
	gotBE := m.Bytes()

	chunk := 2 * ((len(n.Text(16))+3)/4 - 1)
	padded := []byte(input)
	if pad := (chunk - len(padded)%chunk) % chunk; pad > 0 {
		padded = append(padded, make([]byte, pad)...)
	}
	wantBE := make([]byte, chunk)
	for i := 0; i < chunk; i++ {
		wantBE[chunk-1-i] = padded[i]
	}
	gotBE = leftPad(gotBE, chunk)
	if !bytes.Equal(gotBE, wantBE) {
		t.Fatalf("解密后的字节序与预期不符\n got: %x\nwant: %x", truncate(fmt.Sprintf("%x", gotBE), 60), truncate(fmt.Sprintf("%x", wantBE), 60))
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func leftPad(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
