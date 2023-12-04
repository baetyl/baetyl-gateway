package linkmqtt

import (
	"time"

	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/mqtt"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/link"
)

type MqttClient struct {
	*mqtt.Client
	retryTimes int
	retryMsg   chan *link.SendMessage
	id         string
	failMsg    chan []byte
}

func NewClient(cfg config.NorthMQTTConfig) (link.Link, error) {
	var mqttCli *mqtt.Client
	var ops *mqtt.ClientOptions
	var err error

	ops, err = cfg.ToClientOptionsWithPassphrase()
	if err != nil {
		return nil, errors.Trace(err)
	}
	mqttCli = mqtt.NewClient(ops)
	err = mqttCli.Start(NewObserver())
	retryMsg := make(chan *link.SendMessage, 100)
	if err != nil {
		return nil, errors.Trace(err)
	}

	mqttClient := &MqttClient{
		Client:     mqttCli,
		retryTimes: cfg.RetryTimes,
		retryMsg:   retryMsg,
		id:         cfg.ID,
	}
	go mqttClient.resendMsg()
	return mqttClient, nil
}
func (m *MqttClient) Close() error {
	return m.Client.Close()
}

func (m *MqttClient) Send(ctx *comctx.Context, data *link.SendMessage) error {
	var body []byte
	var err error
	if len(data.SendMessage) > 0 {
		body = data.SendMessage
	} else {
		body, err = json.Marshal(data.Message)
		if err != nil {
			return errors.Trace(err)
		}
	}
	startTime := time.Now()
	err = m.PublishWithErr(data.MqttInfo.Qos,
		data.MqttInfo.Topic, body, 0, false, false)
	if err == nil {
		ctx.Logger.Info("mqtt send msg success", log.Any("sendCost", time.Since(startTime)))
	} else {
		m.retryMsg <- data
	}
	return nil
}

func (m *MqttClient) resendMsg() {
	l := log.L()
	for {
		select {
		case data := <-m.retryMsg:
			var body []byte
			var err error
			if len(data.SendMessage) > 0 {
				body = data.SendMessage
			} else {
				body, err = json.Marshal(data.Message)
				if err != nil {
					continue
				}
			}
			for i := 0; i < m.retryTimes; i++ {
				if err := m.PublishWithErr(data.MqttInfo.Qos,
					data.MqttInfo.Topic, body, 0, false, false); err == nil {
					l.Info("mqtt resend msg success")
					break
				} else {
					if i == m.retryTimes-1 {
						cacheMsg, err := json.Marshal(data)
						if err != nil {
							continue
						}
						if cap(m.failMsg) != 0 && (len(m.failMsg) != cap(m.failMsg)) {
							m.failMsg <- cacheMsg
						}
					} else {
						l.Error("mqtt send msg error, start resend msg", log.Any("times", i+1))
					}
				}
			}
		}
	}
}

func (m *MqttClient) SyncSend(ctx *comctx.Context, data []byte) error {
	startTime := time.Now()
	msg := &link.SendMessage{}
	err := json.Unmarshal(data, msg)
	if err != nil {
		ctx.Logger.Error("unmarshal mqtt msg error", log.Any("error", err.Error()))
		return err
	}
	err = m.PublishWithErr(msg.MqttInfo.Qos,
		msg.MqttInfo.Topic, msg.SendMessage, 0, false, false)
	if err == nil {
		ctx.Logger.Info("mqtt send msg success", log.Any("sendCost", time.Since(startTime)))
	}
	return err
}

func (m *MqttClient) GetLinkID() string {
	return m.id
}

func (m *MqttClient) GetFailMsg() chan []byte {
	return m.failMsg
}

func (m *MqttClient) SetFailMsg(fallMsg chan []byte) {
	m.failMsg = fallMsg
}
