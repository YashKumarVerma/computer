package inmemory

import (
	"sync"

	"github.com/yashkumarverma/computer/sdk"
)

type Impl struct {
	mu    sync.RWMutex
	store map[string]any
}

func New(_ *sdk.Config) (*Impl, error) {
	return &Impl{store: make(map[string]any)}, nil
}

func (i *Impl) set(key string, value any) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.store[key] = value
	return nil
}

// get is generic so the three typed getters are one line each and share the
// not-found / wrong-type handling.
func get[T any](i *Impl, key string) (T, error) {
	var zero T
	i.mu.RLock()
	v, ok := i.store[key]
	i.mu.RUnlock()
	if !ok {
		return zero, sdk.ErrNotFound
	}
	t, ok := v.(T)
	if !ok {
		return zero, sdk.ErrWrongType
	}
	return t, nil
}

func (i *Impl) SetString(key, value string) error { return i.set(key, value) }
func (i *Impl) GetString(key string) (string, error) { return get[string](i, key) }

func (i *Impl) SetInt(key string, value int) error { return i.set(key, value) }
func (i *Impl) GetInt(key string) (int, error) { return get[int](i, key) }

func (i *Impl) SetBool(key string, value bool) error { return i.set(key, value) }
func (i *Impl) GetBool(key string) (bool, error) { return get[bool](i, key) }
