package cache

import "io"

const ValueConfFile = "etc/baetyl/conf.yml"

type Cache interface {
	StoreCacheMsg(name string, data []byte) error
	AutoExpiration()
	GetCacheMsg(name string) (map[string][]byte, error)
	DeleteCacheMsg(name string, needToDelete [][]byte) error
	GetAllCacheMsg() (map[string][][]byte, error)
	DeleteTargetMsg(needDeleteKey map[string][][]byte) error
	io.Closer
}
