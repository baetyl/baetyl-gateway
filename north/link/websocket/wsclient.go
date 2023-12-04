package linkws

import (
	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/websocket"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/link"
)

type WebsocketClient struct {
	cli        *websocket.Client
	syncResult chan *websocket.SyncResults
	retryTimes int
	id         string
	failMsg    chan []byte
}

func NewClient(config config.NorthWsConfig) (link.Link, error) {
	ops, err := config.NewTLSConfigClientWithPassphrase()
	if err != nil {
		return nil, err
	}
	c, err := websocket.NewClient(ops, nil)
	if err != nil {
		return nil, err
	}
	syncResult := make(chan *websocket.SyncResults, 100)
	client := &WebsocketClient{
		cli:        c,
		syncResult: syncResult,
		retryTimes: config.RetryTimes,
		id:         config.ID,
	}
	go client.ResultCheck(syncResult)
	return client, nil
}

func (w *WebsocketClient) Send(ctx *comctx.Context, data *link.SendMessage) error {
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
	extra := map[string]any{
		comctx.LogKeyRequestID: ctx.GetRequestID(),
		"body":                 body,
		"times":                data.Times,
	}
	w.cli.SyncSendMsg(body, w.syncResult, extra)
	ctx.TraceCostTime("wsSend sync send")
	ctx.Logger.Info("wsSend success")
	return nil
}
func (w *WebsocketClient) ResultCheck(syncResults chan *websocket.SyncResults) {
	for {
		select {
		case syncResult := <-syncResults:
			l := InitLogInfo(syncResult)
			if syncResult.Err != nil {
				times := 1
				if t, ok := syncResult.Extra["times"]; ok {
					times = t.(int)
				}

				body := []byte{}
				if b, ok := syncResult.Extra["body"]; ok {
					body = b.([]byte)
				}
				if times > w.retryTimes {
					if len(body) > 0 && cap(w.failMsg) != 0 && (len(w.failMsg) != cap(w.failMsg)) {
						w.failMsg <- body
					}
					l.Error("[websocket error]  exceeded maximum retry count  ", log.Any("retryTimes", w.retryTimes), log.Error(syncResult.Err))
					continue
				}
				syncResult.Extra["times"] = times + 1
				l.Error("websocket send  err ,start resend", log.Error(syncResult.Err))
				w.cli.SyncSendMsg(body, w.syncResult, syncResult.Extra)
			} else {
				l.Info("sync send success")
			}
		default:
		}
	}
}

func (w *WebsocketClient) SyncSend(ctx *comctx.Context, data []byte) error {
	err := w.cli.SendMsg(data)
	if err != nil {
		ctx.Logger.Error("websocket msg send  err", log.Any("err", "err"))
		return err
	}
	return nil
}

func InitLogInfo(syncResult *websocket.SyncResults) *log.Logger {
	l := log.L()
	for key, val := range syncResult.Extra {
		if key == "body" {
			continue
		}
		l = l.With(log.Any(key, val))
	}
	l = l.With(log.Any("sendCost", syncResult.SendCost))
	return l
}

func (w *WebsocketClient) GetLinkID() string {
	return w.id
}

func (w *WebsocketClient) GetFailMsg() chan []byte {
	return w.failMsg
}

func (w *WebsocketClient) SetFailMsg(fallMsg chan []byte) {
	w.failMsg = fallMsg
}
