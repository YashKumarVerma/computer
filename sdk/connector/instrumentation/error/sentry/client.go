package sentry

import (
	"github.com/getsentry/sentry-go"
	"github.com/yashkumarverma/computer/sdk"
)

type Impl struct{}

func New(config *sdk.Opts) (*Impl, func(), error) {
	if config.InstrumentationCfg == nil || config.Debug {
		return &Impl{}, func() {}, nil
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              config.InstrumentationCfg.SentryDSN,
		Debug:            config.Debug,
		AttachStacktrace: true,
		Environment:      "sdk",
		Release:          "dev",
		EnableTracing:    true,
		TracesSampleRate: config.InstrumentationCfg.TracingRate,
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return event
		},
	}); err != nil {
		return nil, nil, err
	}

	cleanup := func() { sentry.Flush(config.InstrumentationCfg.Timeout) }
	return &Impl{}, cleanup, nil
}

func (i *Impl) CaptureException(err error) {
	sentry.CaptureException(err)
}
