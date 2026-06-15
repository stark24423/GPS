package wintun

import (
	"context"
	"io"
)

type Loader interface {
	Available() bool
}

type SystemLoader struct{}

type Device interface {
	io.Closer
	Name() string
	ReadPacket(ctx context.Context) ([]byte, error)
	WritePacket(packet []byte) error
}

type Config struct {
	Name          string
	ClientAddress string
	ServerAddress string
	MTU           int
}
