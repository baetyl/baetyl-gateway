package gateway

import (
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/models"
)

type Gateway interface {
	Close()
}

type gatewayImpl struct {
	svr    Server
	driver models.DriverEngine
}

func NewGateway(ctx dm.Context, cfg config.Config) (Gateway, error) {
	var err error
	gateway := &gatewayImpl{}
	// 加载所有驱动配置
	for _, driver := range cfg.Plugin.Drivers {
		err = ctx.LoadDriverConfig(driver.ConfigPath, driver.Name)
		if err != nil {
			return nil, errors.Trace(err)
		}
	}

	// 软网关引擎
	gateway.driver, err = NewDriverEngine(ctx, cfg)
	if err != nil {
		return nil, errors.Trace(err)
	}
	err = gateway.driver.Start()
	if err != nil {
		return nil, errors.Trace(err)
	}
	// 驱动管理server
	gateway.svr, err = NewGatewayServer(cfg, gateway.driver)
	if err != nil {
		return nil, errors.Trace(err)
	}
	gateway.svr.InitRouter()
	go gateway.svr.Run()

	return gateway, nil
}

func (g *gatewayImpl) Close() {
	if g.driver != nil {
		g.driver.Close()
	}
	if g.svr != nil {
		g.svr.Close()
	}
}
