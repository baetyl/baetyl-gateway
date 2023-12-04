package convert

import (
	"encoding/base64"

	"github.com/baetyl/baetyl-go/v2/json"

	"github.com/baetyl/baetyl-gateway/north/link"
)

func MsgBae64Encode(msg *link.SendMessage) error {
	msgByte, err := json.Marshal(msg.Message)
	if err != nil {
		return err
	}
	encodeMsg := base64.StdEncoding.EncodeToString(msgByte)
	msg.SendMessage = []byte(encodeMsg)
	return nil
}
