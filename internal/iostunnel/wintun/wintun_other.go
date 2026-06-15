//go:build !windows

package wintun

func (SystemLoader) Available() bool {
	return false
}
