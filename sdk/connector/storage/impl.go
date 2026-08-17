package storage

import (
	"context"
	"errors"
	"time"

	"github.com/yashkumarverma/computer/sdk/connector/storage/valkey"
)

type Storage interface {
	IncNumberWithEx(ctx context.Context, key string, expiry time.Time) error
	GetNumber(ctx context.Context, key string) (int64, error)
}

type StoreOpts struct {
	ConnStr string
}

func New(opts *StoreOpts) (Storage, error) {
	if opts.ConnStr == "" {
		return nil, errors.New("no connection string provided")
	}

	return valkey.New(&valkey.ValkeyOpts{
		ConnStr: opts.ConnStr,
		UseSSL:  false,
	})
}
