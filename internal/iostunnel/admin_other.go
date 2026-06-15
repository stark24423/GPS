//go:build !windows

package iostunnel

func IsAdministrator() bool {
	return false
}
