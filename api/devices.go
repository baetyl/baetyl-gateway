package api

import (
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/spec/v1"

	"github.com/baetyl/baetyl-gateway/models"
	"github.com/baetyl/baetyl-gateway/south"
)

type DeviceAPI interface {
	GetDeviceShadow(ctx *comctx.Context) (any, error)
	GetLatestDeviceProperty(ctx *comctx.Context) (any, error)
	PublishEvent(ctx *comctx.Context) (any, error)
	SetDeviceNumber(ctx *comctx.Context) (any, error)
}

type deviceImpl struct {
	log    *log.Logger
	driver models.DriverEngine
}

func NewDeviceAPI(driver models.DriverEngine) DeviceAPI {
	return &deviceImpl{
		driver: driver,
		log:    log.With(log.Any("driver", "api")),
	}
}

func (d *deviceImpl) GetLatestDeviceProperty(ctx *comctx.Context) (any, error) {
	name, id := ctx.Param("name"), ctx.Param("id")
	msg := &v1.Message{
		Kind: v1.MessageDevicePropertyGet,
		Metadata: map[string]string{
			"device": name,
		},
		Content: v1.LazyValue{Value: south.BlinkContent{Blink: south.GenPropertyGetBlinkData([]string{id})}},
	}
	err := d.driver.ProcessPropertyGet(ctx, msg)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (d *deviceImpl) PublishEvent(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	evt := new(dm.Event)
	if err := ctx.LoadBody(evt); err != nil {
		return nil, errors.Trace(err)
	}
	msg := &v1.Message{
		Kind: v1.MessageEvent,
		Metadata: map[string]string{
			"device": name,
		},
		Content: v1.LazyValue{Value: evt},
	}
	err := d.driver.ProcessEvent(ctx, msg)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (d *deviceImpl) GetDeviceShadow(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	return d.driver.GetDeviceShadow(ctx, name)
}

func (d *deviceImpl) SetDeviceNumber(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	newShadow := new(models.DeviceMetaShadow)
	if err := ctx.LoadBody(newShadow); err != nil {
		return nil, errors.Trace(err)
	}
	msg := &v1.Message{
		Kind: v1.MessageDeviceDelta,
		Metadata: map[string]string{
			"device": name,
		},
		Content: v1.LazyValue{Value: south.BlinkContent{Blink: south.GenDeltaBlinkData(newShadow.Attributes)}},
	}
	err := d.driver.ProcessDelta(ctx, msg)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
