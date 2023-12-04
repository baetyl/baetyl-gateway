package mqtt

import (
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	v1 "github.com/baetyl/baetyl-go/v2/spec/v1"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/models"
	"github.com/baetyl/baetyl-gateway/south"
)

type Client struct {
	*mqtt.Client
	log *log.Logger
}

func NewMqtt(ctx dm.Context, cfg config.Config) (*Client, error) {
	var mqttCli *mqtt.Client
	var subs []mqtt.QOSTopic
	var ops *mqtt.ClientOptions
	var err error

	for _, driver := range cfg.Plugin.Drivers {
		devices := ctx.GetAllDevices(driver.Name)
		for _, dev := range devices {
			subs = append(subs, dev.DeviceTopic.Delta, dev.DeviceTopic.Event,
				dev.DeviceTopic.GetResponse, dev.DeviceTopic.PropertyGet)
		}
	}

	if cfg.MqttConfig.Address == "" {
		mqttCli, err = ctx.NewSystemBrokerClient(subs)
		if err != nil {
			return nil, errors.Errorf("fail to create system broker client, %s", err.Error())
		}
	} else {
		if len(subs) > 0 {
			cfg.MqttConfig.Subscriptions = append(cfg.MqttConfig.Subscriptions, subs...)
		}
		ops, err = cfg.MqttConfig.ToClientOptions()
		if err != nil {
			return nil, errors.Trace(err)
		}
		mqttCli = mqtt.NewClient(ops)
	}

	return &Client{
		Client: mqttCli,
		log:    log.With(log.Any("mqtt", "client")),
	}, nil
}

func (m *Client) Start(ch chan *models.MsgChan) error {
	err := m.Client.Start(NewObserver(ch, m.log))
	if err != nil {
		m.log.Error("fail to connect mqtt client", log.Any("error", err))
		return err
	}

	return nil
}

// ReportDeviceProperties 召测使用系统mqtt
func (m *Client) ReportDeviceProperties(ctx *comctx.Context, node string, info *dm.DeviceInfo, report v1.Report, topic string) error {
	msg := &v1.Message{
		Kind:     v1.MessageDeviceReport,
		Metadata: genMetadata(node, info),
		Content:  v1.LazyValue{Value: south.BlinkContent{Blink: south.GenPropertyReportBlinkData(report)}},
	}
	pld, err := json.Marshal(msg)
	if err != nil {
		return errors.Trace(err)
	}
	if err = m.Publish(mqtt.QOS(info.DeviceTopic.Report.QOS),
		topic, pld, 0, false, false); err != nil {
		ctx.Logger.Error("fail to push deviceDesire msg", log.Error(err))
		return err
	}
	return nil
}

// Online 报告设备在线状态
func (m *Client) Online(ctx *comctx.Context, node string, info *dm.DeviceInfo) error {
	msg := &v1.Message{
		Kind:     v1.MessageDeviceLifecycleReport,
		Metadata: genMetadata(node, info),
		Content:  v1.LazyValue{Value: south.BlinkContent{Blink: south.GenLifecycleReportBlinkData(true)}},
	}
	pld, err := json.Marshal(msg)
	if err != nil {

		return errors.Trace(err)
	}
	if err = m.Publish(mqtt.QOS(info.DeviceTopic.LifecycleReport.QOS),
		info.DeviceTopic.LifecycleReport.Topic, pld, 0, false, false); err != nil {
		ctx.Logger.Error("fail to push online msg", log.Error(err))
		return err
	}
	return nil
}

// Online 报告设备离线状态
func (m *Client) Offline(ctx *comctx.Context, node string, info *dm.DeviceInfo) error {
	msg := &v1.Message{
		Kind:     v1.MessageDeviceLifecycleReport,
		Metadata: genMetadata(node, info),
		Content:  v1.LazyValue{Value: south.BlinkContent{Blink: south.GenLifecycleReportBlinkData(false)}},
	}
	pld, err := json.Marshal(msg)
	if err != nil {
		return errors.Trace(err)
	}
	if err = m.Publish(mqtt.QOS(info.DeviceTopic.LifecycleReport.QOS),
		info.DeviceTopic.LifecycleReport.Topic, pld, 0, false, false); err != nil {
		ctx.Logger.Error("fail to push offine msg", log.Error(err))
		return err
	}
	return nil
}

func genMetadata(nodeName string, info *dm.DeviceInfo) map[string]string {
	return map[string]string{
		dm.KeyDevice:         info.Name,
		dm.KeyDeviceProduct:  info.DeviceModel,
		dm.KeyAccessTemplate: info.AccessTemplate,
		dm.KeyNode:           nodeName,
		dm.KeyNodeProduct:    dm.NodeProduct,
	}
}

func (m *Client) Close() error {
	return m.Client.Close()
}
