package disk

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"time"

	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/plugin"
	"github.com/baetyl/baetyl-go/v2/utils"
	"github.com/timshannon/bolthold"
	"go.etcd.io/bbolt"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/dataprocessing/cache"
)

type boltCache struct {
	store         *bolthold.Store
	cacheDuration time.Duration
	interval      time.Duration
	log           *log.Logger
	content       context.Context
}

func (ca *boltCache) StoreCacheMsg(name string, data []byte) error {
	if len(data) < 0 {
		ca.log.Error("data to be stored is empty")
		return nil
	}
	err := ca.store.Bolt().Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(name))
		if err != nil {
			return err
		}
		byteSlice := make([]byte, 8)
		binary.BigEndian.PutUint64(byteSlice, uint64(time.Now().UnixNano()/int64(time.Millisecond)))
		if err != nil {
			return err
		}
		err = bucket.Put(byteSlice, data)
		return err
	})
	if err != nil {
		ca.log.Error("store cache msg fail", log.Any("message", string(data)), log.Any("error", err.Error()))
		return err
	}
	return nil
}

func init() {
	plugin.RegisterFactory("disk", New)
}

func New() (plugin.Plugin, error) {
	var cfg config.Config
	if err := utils.LoadYAML(cache.ValueConfFile, &cfg); err != nil {
		return nil, errors.Trace(err)
	}
	return NewDiskCache(cfg.MessageCacheConfig)
}

func NewDiskCache(config config.MessageCacheConfig) (cache.Cache, error) {
	l := log.L().With(log.Any("message cache", "disk"))
	err := os.MkdirAll(filepath.Dir(config.FilePath), 0755)
	if err != nil {
		return nil, errors.New("create file path errors")
	}
	if config.Clear {
		err := os.Remove(config.FilePath)
		if err != nil {
			l.Error("clear old disk cache fail", log.Any("error", err.Error()))
		}
	}
	ops := &bolthold.Options{}
	ops.Options = bbolt.DefaultOptions
	ops.Timeout = time.Second * 10
	sto, err := bolthold.Open(config.FilePath, 0666, ops)
	if err != nil {
		l.Info("the relationship between storage link and bucket failed")
		return nil, err
	}
	bolt := &boltCache{
		cacheDuration: config.CacheDuration,
		store:         sto,
		log:           l.With(log.Any("cache", "disk")),
		content:       context.Background(),
		interval:      config.Interval,
	}
	go bolt.AutoExpiration()
	return bolt, nil
}

func (ca *boltCache) AutoExpiration() {
	timer := time.NewTimer(ca.interval)
	defer timer.Stop()

	for {
		select {
		case <-ca.content.Done():
			return
		case <-timer.C:
			ti := time.Now().Add(-ca.cacheDuration)
			needDeleteKey := make(map[string][][]byte)
			comparisonKey := ti.UnixNano() / int64(time.Millisecond)
			err := ca.store.Bolt().View(func(tx *bbolt.Tx) error {
				return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
					var keyList [][]byte
					err := b.ForEach(func(k, v []byte) error {
						if int64(binary.BigEndian.Uint64(k)) < comparisonKey {
							keyList = append(keyList, k)
						}
						needDeleteKey[string(name)] = keyList
						return nil
					})
					return err
				})
			})
			if err != nil {
				ca.log.Error("failed to obtain expired Key collection")
			}
			err = ca.store.Bolt().Update(func(tx *bbolt.Tx) error {
				for key, ndk := range needDeleteKey {
					bucket := tx.Bucket([]byte(key))
					for _, value := range ndk {
						err := bucket.Delete(value)
						if err != nil {
							ca.log.Error("failed to delete expired Key", log.Any("error", err.Error()))
						}
					}
				}
				if err != nil {
					ca.log.Error("failed to delete expired Key collection")
				}
				return nil
			})
		}
	}
}

func (ca *boltCache) GetCacheMsg(name string) (map[string][]byte, error) {
	kvMap := make(map[string][]byte)
	err := ca.store.Bolt().View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(name))
		if bucket == nil {
			ca.log.Error("bucket is not found")
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			kvMap[string(k)] = v
			return nil
		})
	})
	if err != nil {
		ca.log.Error("failed to obtain data from bucket", log.Any("err", err.Error()))
		return nil, err
	}
	return kvMap, nil
}

func (ca *boltCache) DeleteCacheMsg(name string, needToDelete [][]byte) error {
	var keysToDelete [][]byte
	err := ca.store.Bolt().Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(name))
		if bucket == nil {
			ca.log.Error("this msg send error")
			return nil
		}
		for _, key := range needToDelete {
			err := bucket.Delete(key)
			if err != nil {
				keysToDelete = append(keysToDelete, key)
				return err
			}
		}
		return nil
	})
	if len(keysToDelete) != 0 {
		err := ca.store.Bolt().Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte(name))
			if bucket == nil {
				ca.log.Error("this msg send error")
				return nil
			}
			for _, key := range keysToDelete {
				err := bucket.Delete(key)
				if err != nil {
					return err
				}
			}
			return nil
		})
		ca.log.Error("failed to delete deleted data", log.Any("err", err.Error()))
		return err
	}
	if err != nil {
		ca.log.Error("failed to delete data in bucket", log.Any("err", err.Error()))
		return err
	}
	return nil
}

func (ca *boltCache) GetAllCacheMsg() (map[string][][]byte, error) {
	kvMap := make(map[string][][]byte)
	err := ca.store.Bolt().View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			var keyList [][]byte
			err := b.ForEach(func(k, v []byte) error {
				keyList = append(keyList, v)
				return nil
			})
			kvMap[string(name)] = keyList
			return err
		})
	})
	if err != nil {
		ca.log.Error("failed to obtain all cached data")
		return nil, err
	}
	return kvMap, nil
}

func (ca *boltCache) DeleteTargetMsg(needDeleteKey map[string][][]byte) error {
	err := ca.store.Bolt().Update(func(tx *bbolt.Tx) error {
		for key, ndk := range needDeleteKey {
			bucket := tx.Bucket([]byte(key))
			for _, value := range ndk {
				err := bucket.Delete(value)
				if err != nil {
					ca.log.Error("failed to delete target Key", log.Any("error", err.Error()))
				}
			}
		}
		return nil
	})
	if err != nil {
		ca.log.Error("failed to delete target data", log.Any("error", err.Error()))
		return err
	}
	return nil
}

func (ca *boltCache) Close() error {
	ca.content.Done()
	return nil
}
