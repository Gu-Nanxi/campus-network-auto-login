package main

import (
	"fmt"
	"math/big"
	"strings"
)

// rsaEncryptJS 复刻门户 security.js 里 RSAUtils.encryptedString 的行为：
//
//   - 没有 PKCS#1 填充，只是把字符串按字节、以"小端"顺序塞进大整数；
//   - 每块长度 = 模数字节数 - 2（1024 位模数 => 每块 126 字节）；
//   - 不足整块时在尾部补 0x00；
//   - 输出是每块 4*digits 位十六进制，多块之间用空格连接。
//
// 注意：明文进这一层之前要先反转，反转是调用方（门户 JS）做的。
func rsaEncryptJS(exponent, modulus *big.Int, plain string) (string, error) {
	if exponent == nil || modulus == nil {
		return "", fmt.Errorf("缺少公钥")
	}
	modHex := modulus.Text(16)
	digits := (len(modHex) + 3) / 4
	chunk := 2 * (digits - 1)
	if chunk <= 0 {
		return "", fmt.Errorf("模数过小：%d 位", len(modHex)*4)
	}

	raw := []byte(plain)
	if pad := (chunk - len(raw)%chunk) % chunk; pad > 0 {
		raw = append(raw, make([]byte, pad)...)
	}

	var parts []string
	for i := 0; i < len(raw); i += chunk {
		block := raw[i : i+chunk]
		// JS 里 limb[j] = a[2j] + (a[2j+1] << 8)，等价于把小端字节序当作整数
		be := make([]byte, chunk)
		for j := 0; j < chunk; j++ {
			be[chunk-1-j] = block[j]
		}
		m := new(big.Int).SetBytes(be)
		c := new(big.Int).Exp(m, exponent, modulus)

		h := c.Text(16)
		if need := 4 * digits; len(h) < need {
			h = strings.Repeat("0", need-len(h)) + h
		}
		parts = append(parts, h)
	}
	return strings.Join(parts, " "), nil
}

func reverseString(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
