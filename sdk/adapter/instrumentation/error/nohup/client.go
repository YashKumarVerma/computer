package nohupErrReporting

import (
	"github.com/yashkumarverma/computer/sdk"
	"go.uber.org/zap"
)

type Impl struct{}

func New(_ *sdk.Config) (*Impl, func(), error) {
	log := sdk.GetAppLogger()
	log.Info("nohup error reporting init")

	cleanup := func() { log.Info("nohup flush") }
	return &Impl{}, cleanup, nil
}

func (i *Impl) CaptureException(err error) {
	sdk.GetAppLogger().Error("error reported", zap.Error(err))
}
