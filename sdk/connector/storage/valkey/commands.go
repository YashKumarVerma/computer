package valkey

import (
	"context"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

func (i *Impl) key(k string) string { return i.prefix + k }

func (i *Impl) set(key, value string) error {
	ctx := context.Background()
	return i.client.Do(ctx, i.client.B().Set().Key(i.key(key)).Value(value).Build()).Error()
}

func (i *Impl) IncNumberWithEx(ctx context.Context, key string, expiry time.Time) error {
	key = i.key(key)
	cmd := i.client.B().Increx().Key(key).ExatTimestamp(expiry.Unix()).Enx().Build()
	return i.client.Do(ctx, cmd).Error()
}

func (i *Impl) GetNumber(ctx context.Context, key string) (int64, error) {
	cmd := i.client.B().Get().Key(i.key(key)).Build()
	n, err := i.client.Do(ctx, cmd).AsInt64()
	if valkeygo.IsValkeyNil(err) {
		// Missing bucket is normal (no events yet); report 0, not an error, so
		// callers can distinguish "no data" from a real backend failure.
		return 0, nil
	}
	return n, err
}
