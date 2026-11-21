//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Credential 只存在内存和 DPAPI 加密文件里，不落明文、不进日志。
type Credential struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

// CRYPTPROTECT_UI_FORBIDDEN = 0x1：后台进程不弹任何 UI。
const cryptProtectUIForbidden = 0x1

func dpapiProtect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("待加密数据为空")
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob

	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData 失败: %v", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	buf := make([]byte, out.cbData)
	copy(buf, unsafe.Slice(out.pbData, int(out.cbData)))
	return buf, nil
}

func dpapiUnprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("待解密数据为空")
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob

	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 失败（文件可能来自别的用户或别的机器）: %v", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	buf := make([]byte, out.cbData)
	copy(buf, unsafe.Slice(out.pbData, int(out.cbData)))
	return buf, nil
}

func saveCredential(path string, c Credential) error {
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := dpapiProtect(plain)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return err
	}
	return nil
}

func loadCredential(path string) (*Credential, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("凭据文件不存在，先执行 -mode setup")
		}
		return nil, err
	}
	plain, err := dpapiUnprotect(raw)
	if err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, fmt.Errorf("凭据内容损坏: %w", err)
	}
	if c.User == "" || c.Password == "" {
		return nil, errors.New("凭据内容不完整")
	}
	return &c, nil
}
