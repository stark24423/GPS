package elevation

import "errors"

var ErrRelaunched = errors.New("process relaunched as administrator")

func EnsureAdministrator() error {
	return ensureAdministrator()
}
