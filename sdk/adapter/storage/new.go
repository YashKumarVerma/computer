package storage

import (
	"github.com/yashkumarverma/computer/sdk"
	"github.com/yashkumarverma/computer/sdk/adapter/storage/inmemory"
	"github.com/yashkumarverma/computer/sdk/adapter/storage/valkey"
)

type Storage interface {
	SetString(key, value string) error
	GetString(key string) (string, error)
	SetInt(key string, value int) error
	GetInt(key string) (int, error)
	SetBool(key string, value bool) error
	GetBool(key string) (bool, error)
}

func New(config *sdk.Config) (Storage, error) {
	if config.ValkeyCfg == nil {
		return inmemory.New(config)
	}

	return valkey.New(config)
}
