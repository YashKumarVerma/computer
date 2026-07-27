package errInstrumentation

import (
	"github.com/yashkumarverma/computer/sdk"
	nohupErrReporting "github.com/yashkumarverma/computer/sdk/adapter/instrumentation/error/nohup"
	"github.com/yashkumarverma/computer/sdk/adapter/instrumentation/error/sentry"
)

type Reporter interface {
	CaptureException(err error)
}

func New(config *sdk.Config) (Reporter, func(), error) {
	if config.InstrumentationCfg == nil {
		return nohupErrReporting.New(config)
	}

	if config.InstrumentationCfg.SentryDSN == "" {
		return nohupErrReporting.New(config)
	}

	return sentry.New(config)
}
