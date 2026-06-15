//go:build windows

package iostunnel

import "syscall"

func IsAdministrator() bool {
	proc := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin")
	ret, _, _ := proc.Call()
	return ret != 0
}
