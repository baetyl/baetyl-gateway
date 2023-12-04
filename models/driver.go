package models

import (
	plugin "github.com/baetyl/baetyl-gateway-sdk/sdk/golang"
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/spec/v1"

	"github.com/baetyl/baetyl-gateway/config"
)

type DriverEngine interface {
	Start() error
	Close()
	ProcessPropertyGet(ctx *comctx.Context, msg *v1.Message) error
	ProcessEvent(ctx *comctx.Context, mgx *v1.Message) error
	ProcessDelta(ctx *comctx.Context, mgx *v1.Message) error
	GetAllConfigs(ctx *comctx.Context, drivers []string) *AllPluginConfigs
	GetDevicesStatusByDriver(ctx *comctx.Context, driverName string) map[string]int
	GetDeviceShadow(ctx *comctx.Context, deviceName string) (map[string]dm.ReportProperty, error)
	GetPluginConfigPath(ctx *comctx.Context, driverName string) string
	StartDriver(ctx *comctx.Context, client *plugin.Client, driverName string, path string) error
	UpdateNorthLink(linkConfig *config.UplinkConfig) error
	ContextReload(driverName string, path string) error
}
