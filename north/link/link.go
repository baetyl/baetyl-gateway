package link

import (
	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	"github.com/baetyl/baetyl-go/v2/spec/v1"
)

type SendMessage struct {
	Message     *v1.Message
	SendMessage []byte
	MqttInfo    MqttInfo
	Times       int
}

type MqttInfo struct {
	Topic string
	Qos   mqtt.QOS
}

type Link interface {
	Send(ctx *comctx.Context, data *SendMessage) error
	SyncSend(ctx *comctx.Context, data []byte) error
	GetLinkID() string
	GetFailMsg() chan []byte
	SetFailMsg(chan []byte)
}
