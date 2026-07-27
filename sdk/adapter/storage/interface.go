package storage

type Storage interface {
	SetString(key string, value string) error
	GetString(key string) (string, error)
}
