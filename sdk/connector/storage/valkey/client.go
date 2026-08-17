package valkey

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

type Impl struct {
	client valkeygo.Client
	prefix string
}

type ValkeyOpts struct {
	ConnStr   string
	UseSSL    bool
	TLSDomain string
	KeyPrefix string
}

func New(opts *ValkeyOpts) (*Impl, error) {
	opt, err := valkeygo.ParseURL(opts.ConnStr)
	if err != nil {
		return nil, err
	}
	if opts.UseSSL {
		opt.TLSConfig = &tls.Config{ServerName: opts.TLSDomain}
	}

	client, err := valkeygo.NewClient(opt)
	if err != nil {
		return nil, err
	}
	if err := checkCapabilities(client); err != nil {
		client.Close()
		return nil, err
	}
	return &Impl{client: client, prefix: opts.KeyPrefix}, nil
}

// ErrCapabilityMissing is returned by New when the server lacks a required command.
var ErrCapabilityMissing = errors.New("server capability missing")

func checkCapabilities(client valkeygo.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Do(ctx, client.B().Arbitrary("COMMAND", "INFO", "increx").Build()).ToArray()
	if err != nil {
		return err
	}
	if len(resp) == 0 || resp[0].IsNil() {
		return fmt.Errorf("%w: INCREX not supported, upgrade to Valkey 8+ or Redis 8.8+", ErrCapabilityMissing)
	}
	return nil
}
