package bootstrap

import (
	"context"
	"errors"

	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	appmysql "github.com/kalaGN/gin-finapi/pkg/mysql"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/sirupsen/logrus"
)

// configurationRuntime 集中配置源、回源和同步的装配，不承载 HTTP 或 Mongo 查询。
type configurationRuntime struct {
	reader    *configdata.Reader
	mysql     *appmysql.Store
	keys      *appredis.APIKeys
	maps      *appredis.ProductMaps
	publisher *appredis.ConfigPublisher
	syncer    *configdata.Synchronizer
}

func newConfiguration(cfg config.AppConfig, log *logrus.Logger) (result *configurationRuntime, err error) {
	c := &configurationRuntime{}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.close(context.Background()))
		}
	}()
	c.maps, err = appredis.NewProductMaps(cfg.Redis)
	if err != nil {
		return nil, err
	}
	c.keys, err = appredis.NewAPIKeys(cfg.Redis)
	if err != nil {
		return nil, err
	}
	credentials := configdata.NamedSource("credentials", "redis", configdata.Source[string, services.Credential](c.keys.ReadCredential))
	products := configdata.NamedSource("product_access", "redis", configdata.Source[services.ProductKey, services.ProductAccess](c.keys.ReadProductAccess))
	mappings := configdata.NamedSource("product_map", "redis", configdata.Source[int, map[string]int](c.maps.ReadProductMap))
	budget := cfg.Redis.Timeout
	if cfg.MySQL.Enabled {
		c.mysql, err = appmysql.NewStore(cfg.MySQL)
		if err != nil {
			return nil, err
		}
		budget += cfg.MySQL.QueryTimeout
		credentials = configdata.Fallback(credentials, configdata.NamedSource("credentials", "mysql", configdata.Source[string, services.Credential](c.mysql.ReadCredential)), fallbackReporter(log, "credential"))
		products = configdata.Fallback(products, configdata.NamedSource("product_access", "mysql", configdata.Source[services.ProductKey, services.ProductAccess](c.mysql.ReadProductAccess)), fallbackReporter(log, "product_access"))
		mappings = configdata.Fallback(mappings, configdata.NamedSource("product_map", "mysql", configdata.Source[int, map[string]int](c.mysql.ReadProductMap)), fallbackReporter(log, "product_map"))
	}
	c.reader, err = configdata.New(configdata.Sources{
		Credentials: []configdata.Source[string, services.Credential]{credentials},
		Products:    []configdata.Source[services.ProductKey, services.ProductAccess]{products},
		Maps:        []configdata.Source[int, map[string]int]{mappings},
	}, cfg.Cache, budget)
	if err != nil {
		return nil, err
	}
	if cfg.MySQL.Enabled && cfg.MySQL.SyncEnabled {
		c.publisher, err = appredis.NewConfigPublisher(cfg.Redis)
		if err != nil {
			return nil, err
		}
		c.syncer, err = configdata.NewSynchronizer(c.mysql.Snapshot, c.publisher, configdata.SyncOptions{Interval: cfg.MySQL.SyncInterval, Timeout: cfg.MySQL.SyncTimeout, ReleaseTimeout: cfg.Redis.Timeout}, func(ctx context.Context) {
			stats := c.reader.Refresh(ctx)
			if stats.Failed > 0 && log != nil {
				log.WithFields(logrus.Fields{"event": "config_refresh_after_sync_failed", "failed": stats.Failed}).Warn("同步后部分内存快照刷新失败")
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func fallbackReporter(log *logrus.Logger, dataType string) func(string) {
	return func(reason string) {
		if log == nil {
			return
		}
		entry := log.WithFields(logrus.Fields{"event": "config_source_fallback", "data_type": dataType, "reason": reason})
		if reason == "missing" {
			entry.Debug("Redis 未命中，读取 MySQL")
		} else {
			entry.Warn("Redis 配置读取失败，回源 MySQL")
		}
	}
}

func (c *configurationRuntime) start(log *logrus.Logger) {
	c.reader.Start(func(stats configdata.RefreshStats) {
		if log == nil {
			return
		}
		entry := log.WithFields(logrus.Fields{"checked": stats.Checked, "failed": stats.Failed, "duration_ms": stats.Duration.Milliseconds()})
		if stats.Failed > 0 {
			entry.Warn("配置缓存刷新部分失败，已按错误分类保留或清除快照")
		} else {
			entry.Debug("配置缓存刷新完成")
		}
	})
	if c.syncer != nil {
		c.syncer.Start(func(stats configdata.SyncStats, err error) {
			if log == nil {
				return
			}
			entry := log.WithFields(logrus.Fields{"event": "mysql_redis_sync", "credentials": stats.Credentials, "products": stats.Products, "mappings": stats.Mappings, "removed": stats.Removed, "duration_ms": stats.Duration.Milliseconds()})
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					entry.Error("MySQL 到 Redis 同步失败，请检查配置源、连接和租约")
				}
				return
			}
			if stats.Skipped {
				entry.Debug("其他实例持有配置同步租约，本轮跳过")
			} else {
				entry.Info("MySQL 配置已同步 Redis")
			}
		})
	}
}

func (c *configurationRuntime) stop(ctx context.Context) error {
	var errs []error
	if c.syncer != nil {
		errs = append(errs, c.syncer.Close(ctx))
	}
	if c.reader != nil {
		errs = append(errs, c.reader.Close(ctx))
	}
	return errors.Join(errs...)
}
func (c *configurationRuntime) closeMySQL() error {
	if c.mysql != nil {
		return c.mysql.Close()
	}
	return nil
}
func (c *configurationRuntime) closeRedis() error {
	var errs []error
	if c.publisher != nil {
		errs = append(errs, c.publisher.Close())
	}
	if c.keys != nil {
		errs = append(errs, c.keys.Close())
	}
	if c.maps != nil {
		errs = append(errs, c.maps.Close())
	}
	return errors.Join(errs...)
}
func (c *configurationRuntime) close(ctx context.Context) error {
	return errors.Join(c.stop(ctx), c.closeMySQL(), c.closeRedis())
}
