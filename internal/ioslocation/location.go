package ioslocation

import (
	"context"
	"errors"
	"time"

	"gpssim/internal/core"
	"gpssim/internal/iostunnel"
)

var ErrDVTLocationPending = errors.New("DVT LocationSimulation over built-in tunnel is not complete yet")

type Client struct {
	Tunnel *iostunnel.Manager
}

func New(manager *iostunnel.Manager) *Client {
	return &Client{Tunnel: manager}
}

func (c *Client) SetLocation(ctx context.Context, udid string, point core.Coordinate) error {
	if _, err := c.Tunnel.EnsureRunning(ctx, udid); err != nil {
		return err
	}
	return ErrDVTLocationPending
}

func (c *Client) PlayRoute(ctx context.Context, udid string, points []core.Coordinate, tick time.Duration) error {
	if len(points) == 0 {
		return nil
	}
	if _, err := c.Tunnel.EnsureRunning(ctx, udid); err != nil {
		return err
	}
	return ErrDVTLocationPending
}

func (c *Client) ClearLocation(ctx context.Context, udid string) error {
	return c.Tunnel.Stop(udid)
}
