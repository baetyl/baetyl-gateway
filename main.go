package main

import (
	_ "net/http/pprof"

	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/gateway"
)

func main() {
	dm.Run(func(ctx dm.Context) error {
		var cfg config.Config
		// 加载主配置
		err := ctx.LoadCustomConfig(&cfg)
		if err != nil {
			return errors.Trace(err)
		}
		// 软网关主处理
		g, err := gateway.NewGateway(ctx, cfg)
		if err != nil {
			return errors.Trace(err)
		}
		defer g.Close()

		ctx.Wait()
		return nil
	})
}
