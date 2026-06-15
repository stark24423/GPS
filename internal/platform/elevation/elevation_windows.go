//go:build windows

package elevation

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const swNormal = 1

func ensureAdministrator() error {
	if isAdministrator() {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable for elevation: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory for elevation: %w", err)
	}

	// ShellExecuteW 的 "runas" verb 會交給 Windows 顯示 UAC。
	// 非管理員程序只負責重新啟動自己，避免後續建立 Wintun adapter 時才失敗。
	ret, _, callErr := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("runas"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(exe))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(joinArgs(os.Args[1:])))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(wd))),
		swNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("request administrator privileges failed: ShellExecuteW=%d (%v)", ret, callErr)
	}
	return ErrRelaunched
}

func isAdministrator() bool {
	ret, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin").Call()
	return ret != 0
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}
