package linkmqtt

import (
	"github.com/256dpi/gomqtt/packet"
	"github.com/baetyl/baetyl-go/v2/mqtt"
)

type observer struct {
}

func NewObserver() mqtt.Observer {
	return &observer{}
}

func (o *observer) OnPublish(_ *packet.Publish) error {
	return nil
}

func (o *observer) OnPuback(_ *packet.Puback) error {
	return nil
}

func (o *observer) OnError(err error) {
	return
}
