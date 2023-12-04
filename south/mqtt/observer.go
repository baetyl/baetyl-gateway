package mqtt

import (
	"github.com/256dpi/gomqtt/packet"
	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	"github.com/baetyl/baetyl-go/v2/spec/v1"

	"github.com/baetyl/baetyl-gateway/models"
)

type observer struct {
	msgChs chan *models.MsgChan
	log    *log.Logger
}

func NewObserver(msgChs chan *models.MsgChan, log *log.Logger) mqtt.Observer {
	return &observer{msgChs: msgChs, log: log}
}

func (o *observer) OnPublish(pkt *packet.Publish) error {
	var msg v1.Message
	if err := json.Unmarshal(pkt.Message.Payload, &msg); err != nil {
		o.log.Error("failed to unmarshal message",
			log.Any("payload", string(pkt.Message.Payload)))
		return nil
	}

	requestID := ""
	if logID, ok := msg.Metadata[comctx.LogKeyRequestID]; ok {
		requestID = logID
	}
	msgChan := models.MsgChan{
		Msg: &msg,
		Ctx: comctx.NewTraceContext(requestID, log.Any("action", "downLinkMsg")),
	}
	select {
	case o.msgChs <- &msgChan:
	default:
		o.log.Error("failed to write device message", log.Any("msg", msg))
	}
	return nil
}

func (o *observer) OnPuback(pkt *packet.Puback) error {
	return nil
}

func (o *observer) OnError(err error) {
	o.log.Error("receive message error", log.Any("error", err))
}
