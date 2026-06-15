//go:build !windows

package elevation

func ensureAdministrator() error {
	return nil
}
