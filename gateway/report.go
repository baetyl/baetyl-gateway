package gateway

import (
	"fmt"

	plugin "github.com/baetyl/baetyl-gateway-sdk/sdk/golang"
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/spec/v1"

	"github.com/baetyl/baetyl-gateway/models"
)

var _ plugin.Report = &ReportImpl{}

type ReportImpl struct {
	msgCh map[string]*driverControl
}

func NewReport(ch map[string]*driverControl) *ReportImpl {
	return &ReportImpl{
		msgCh: ch,
	}
}

// Post 驱动数据采集上报接口，驱动调用，采集消息放入channel
func (m *ReportImpl) Post(req *plugin.Request) (*plugin.Response, error) {
	var devName, driverName string
	var ok bool
	msg := &v1.Message{}
	err := json.Unmarshal([]byte(req.Req), msg)
	if err != nil {
		return nil, err
	}
	if driverName, ok = msg.Metadata[dm.KeyDriverName]; !ok {
		return nil, errors.New("failed to get driverName in report msg")
	}
	if devName, ok = msg.Metadata[dm.KeyDeviceName]; !ok {
		return nil, errors.New("failed to get deviceName in report msg")
	}
	msgChan := &models.MsgChan{
		Msg: msg,
		Ctx: comctx.NewTraceContext(req.RequestID, log.Any("action", "post"), log.Any("gateway", "report")),
	}

	switch msg.Kind {
	case v1.MessageDeviceReport, v1.MessageDeviceDesire:
		select {
		case m.msgCh[driverName].uplinkCh[devName] <- msgChan:
		default:
			msgChan.Ctx.Logger.Error("failed to write device report message", log.Any("msg", msg))
		}
	default:
		msgChan.Ctx.Logger.Error("message kind not supported", log.Any("type", msg.Kind))
	}

	return &plugin.Response{Data: fmt.Sprintf("msg kind: %s post success", msg.Kind)}, nil
}

// State 驱动状态上报接口，驱动调用，消息放入channel
func (m *ReportImpl) State(req *plugin.Request) (*plugin.Response, error) {
	var devName, driverName string
	var ok bool
	msg := &v1.Message{}
	err := json.Unmarshal([]byte(req.Req), msg)
	if err != nil {
		return nil, err
	}
	if driverName, ok = msg.Metadata[dm.KeyDriverName]; !ok {
		return nil, errors.New("failed to get driverName in report msg")
	}
	if devName, ok = msg.Metadata[dm.KeyDeviceName]; !ok {
		return nil, errors.New("failed to get deviceName in state msg")
	}
	msgChan := &models.MsgChan{
		Msg: msg,
		Ctx: comctx.NewTraceContext(req.RequestID, log.Any("action", "state"), log.Any("gateway", "state")),
	}

	switch msg.Kind {
	case v1.MessageDeviceLifecycleReport:
		select {
		case m.msgCh[driverName].uplinkCh[devName] <- msgChan:
		default:
			msgChan.Ctx.Logger.Error("failed to write device state message", log.Any("msg", msg), log.Any("gateway", "report"))
		}
	default:
		msgChan.Ctx.Logger.Error("message kind not supported", log.Any("type", msg.Kind))
	}
	return &plugin.Response{Data: fmt.Sprintf("msg kind: %s state success", msg.Kind), RequestID: msgChan.Ctx.GetRequestID()}, nil
}
