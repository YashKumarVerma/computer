package valkey

import (
	"context"
	"crypto/tls"
	"strconv"

	valkeygo "github.com/valkey-io/valkey-go"
	"github.com/yashkumarverma/computer/sdk"
)

type Impl struct {
	client valkeygo.Client
	prefix string
}

func New(cfg *sdk.Config) (*Impl, error) {
	opt, err := valkeygo.ParseURL(cfg.ValkeyCfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.ValkeyCfg.UseSsl {
		opt.TLSConfig = &tls.Config{ServerName: cfg.ValkeyCfg.TLSDomain}
	}
	// ponytail: MaxConns/IdleConns/PipelineSize don't map onto valkey-go's pool
	// model; wire opt.BlockingPoolSize/PipelineMultiplex here if tuning is needed.

	client, err := valkeygo.NewClient(opt) // dials now, fails fast if unreachable
	if err != nil {
		return nil, err
	}
	return &Impl{client: client, prefix: cfg.ValkeyCfg.KeyPrefix}, nil
}

func (i *Impl) key(k string) string { return i.prefix + k }

func (i *Impl) set(key, value string) error {
	ctx := context.Background()
	return i.client.Do(ctx, i.client.B().Set().Key(i.key(key)).Value(value).Build()).Error()
}

// get returns the raw string, mapping a valkey nil to sdk.ErrNotFound.
func (i *Impl) get(key string) (string, error) {
	ctx := context.Background()
	v, err := i.client.Do(ctx, i.client.B().Get().Key(i.key(key)).Build()).ToString()
	if valkeygo.IsValkeyNil(err) {
		return "", sdk.ErrNotFound
	}
	return v, err
}

func (i *Impl) SetString(key, value string) error { return i.set(key, value) }
func (i *Impl) GetString(key string) (string, error) { return i.get(key) }

func (i *Impl) SetInt(key string, value int) error { return i.set(key, strconv.Itoa(value)) }
func (i *Impl) GetInt(key string) (int, error) {
	v, err := i.get(key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, sdk.ErrWrongType
	}
	return n, nil
}

func (i *Impl) SetBool(key string, value bool) error { return i.set(key, strconv.FormatBool(value)) }
func (i *Impl) GetBool(key string) (bool, error) {
	v, err := i.get(key)
	if err != nil {
		return false, err
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, sdk.ErrWrongType
	}
	return b, nil
}
