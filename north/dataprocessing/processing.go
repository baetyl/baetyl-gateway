package dataprocessing

import (
	"github.com/baetyl/baetyl-go/v2/errors"

	"github.com/baetyl/baetyl-gateway/north/dataprocessing/convert"
	"github.com/baetyl/baetyl-gateway/north/link"
)

type DataProcessFunc func(msg *link.SendMessage) error

type DataFunc struct {
	FuncList map[string]DataProcessFunc
}

var DataProcess DataFunc

func init() {
	DataProcess = DataFunc{
		FuncList: map[string]DataProcessFunc{
			"MsgBae64Encode": convert.MsgBae64Encode,
		},
	}
}

func (d *DataFunc) RunFunc(name string, msg *link.SendMessage) error {
	if runFunc, ok := d.FuncList[name]; ok {
		return runFunc(msg)
	}
	return errors.Trace(errors.New("func name not exit"))
}
