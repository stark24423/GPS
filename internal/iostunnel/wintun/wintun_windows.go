//go:build windows

package wintun

import "syscall"

func (SystemLoader) Available() bool {
	// 只檢查 DLL 是否能被 Windows loader 找到；真正建立 adapter 會在 tunnel transport 階段處理。
	dll := syscall.NewLazyDLL("wintun.dll")
	return dll.Load() == nil
}
