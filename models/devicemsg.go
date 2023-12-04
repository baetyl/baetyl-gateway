package models

import (
	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/spec/v1"
)

type MsgChan struct {
	Msg *v1.Message
	Ctx *comctx.Context
}
