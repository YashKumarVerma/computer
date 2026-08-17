package orphanCP

import (
	"github.com/yashkumarverma/computer/sdk"
)

type Impl struct {
}

func New(_ *sdk.Opts) (*Impl, error) {
	return &Impl{}, nil
}
