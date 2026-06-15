package wintun

type Loader interface {
	Available() bool
}

type SystemLoader struct{}
