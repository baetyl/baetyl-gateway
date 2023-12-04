package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/plugin"
	"github.com/baetyl/baetyl-go/v2/utils"
	"github.com/coocood/freecache"
	"github.com/dustin/go-humanize"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/dataprocessing/cache"
)

type freeMemoryCache struct {
	store         *freecache.Cache
	cacheDuration time.Duration
	log           *log.Logger
	content       context.Context
	interval      time.Duration
}

var keyMap sync.Map

func init() {
	plugin.RegisterFactory("memory", New)
}

func New() (plugin.Plugin, error) {
	var cfg config.Config
	if err := utils.LoadYAML(cache.ValueConfFile, &cfg); err != nil {
		return nil, errors.Trace(err)
	}
	return NewFreeMemoryCache(cfg.MessageCacheConfig)
}

func NewFreeMemoryCache(config config.MessageCacheConfig) (cache.Cache, error) {
	bytes, err := humanize.ParseBytes(config.MaxMemoryUse)
	if err != nil {
		log.L().Error("unit conversion failed", log.Any("err", err.Error()))
	}
	meCache := freecache.NewCache(int(bytes))
	fmc := &freeMemoryCache{
		store:         meCache,
		cacheDuration: config.CacheDuration,
		log:           log.L().With(log.Any("message cache", "memory")),
		interval:      config.Interval,
		content:       context.Background(),
	}

	go fmc.AutoExpiration()
	return fmc, nil
}

func (fmc *freeMemoryCache) StoreCacheMsg(name string, data []byte) error {
	cacheDuration := fmc.cacheDuration
	expire := int(cacheDuration.Seconds())
	randKey := utils.RandString(10)
	key := fmt.Sprintf("%s%s", name, randKey)
	keyMap.Store(key, "")
	err := fmc.store.Set([]byte(key), data, expire)
	if err != nil {
		fmc.log.Error("caching data to memory failed", log.Any("err", err.Error()))
		return err
	}
	return nil
}

func (fmc *freeMemoryCache) GetCacheMsg(name string) (map[string][]byte, error) {
	count := fmc.store.EntryCount()
	kvMap := make(map[string][]byte)
	iterator := fmc.store.NewIterator()
	var i int64
	for i = 0; i < count; i++ {
		next := iterator.Next()
		if next != nil && strings.Contains(string(next.Key), name) {
			kvMap[string(next.Key)] = next.Value
		}
	}
	return kvMap, nil
}

func (fmc *freeMemoryCache) DeleteCacheMsg(_ string, needToDelete [][]byte) error {
	var reDelete [][]byte
	for _, nd := range needToDelete {
		sign := fmc.store.Del(nd)
		if !sign {
			reDelete = append(reDelete, nd)
		}
		keyMap.Delete(string(nd))
	}
	if len(reDelete) == 0 {
		fmc.log.Info("successfully cleared the data that needs to be deleted")
		return nil
	}
	for _, nd := range reDelete {
		fmc.store.Del(nd)
	}
	return nil
}

func (fmc *freeMemoryCache) AutoExpiration() {
	timer := time.NewTimer(fmc.interval)
	defer timer.Stop()
	for {
		select {
		case <-fmc.content.Done():
			return
		case <-timer.C:
			var keyArray []string
			keyMap.Range(func(key, value interface{}) bool {
				keys, ok := key.(string)
				if ok {
					value, err := fmc.store.Get([]byte(keys))
					if err != nil {
						fmc.log.Error("failed to obtain the corresponding value")
					}
					if len(value) == 0 {
						keyArray = append(keyArray, keys)
					}
				} else {
					fmc.log.Error("failed to convert stored data back to byte array")
				}

				return true
			})
			for _, key := range keyArray {
				keyMap.Delete(key)
			}
		}
	}
}

func (fmc *freeMemoryCache) GetAllCacheMsg() (map[string][][]byte, error) {
	return nil, nil
}

func (fmc *freeMemoryCache) DeleteTargetMsg(_ map[string][][]byte) error {
	return nil
}

func (fmc *freeMemoryCache) Close() error {
	fmc.content.Done()
	return nil
}
