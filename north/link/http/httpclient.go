package linkhttp

import (
	"bytes"

	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/http"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/link"
)

type httpClient struct {
	cli        *http.Client
	path       string
	method     string
	syncResult chan *http.SyncResults
	header     map[string]string
	retryTimes int
	id         string
	failMsg    chan []byte
}

func NewClient(config config.NorthHTTPConfig) (link.Link, error) {
	ops, err := config.ToClientOptionsWithPassphrase()
	if err != nil {
		return nil, err
	}
	c := http.NewClient(ops)
	syncResult := make(chan *http.SyncResults, 100)
	client := &httpClient{
		cli:        c,
		path:       config.Path,
		method:     config.Method,
		syncResult: syncResult,
		retryTimes: config.RetryTimes,
		id:         config.ID,
	}
	go client.ResultCheck(syncResult)
	return client, nil
}

func (h *httpClient) Send(ctx *comctx.Context, data *link.SendMessage) error {
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
	h.cli.SyncSendUrl(h.method, h.path, bytes.NewReader(body), h.syncResult, extra, h.header)
	ctx.TraceCostTime("http send")
	return nil
}

func (h *httpClient) SyncSend(ctx *comctx.Context, data []byte) error {
	response, err := h.cli.SendUrl(h.method, h.path, bytes.NewReader(data), h.header)
	if response != nil {
		response.Body.Close()
	}
	ctx.TraceCostTime("http msg send")
	return err
}

func (h *httpClient) ResultCheck(syncResults chan *http.SyncResults) {
	for {
		select {
		case syncResult := <-syncResults:
			l := InitLogInfo(syncResult)
			if syncResult.Err != nil {
				times := 1
				if t, ok := syncResult.Extra["times"]; ok {
					times = t.(int)
				}
				var body []byte
				if b, ok := syncResult.Extra["body"]; ok {
					body = b.([]byte)
				}
				if times > h.retryTimes {
					if len(body) > 0 && cap(h.failMsg) != 0 && (len(h.failMsg) != cap(h.failMsg)) {
						h.failMsg <- body
					}
					l.Error("[http error]  exceeded maximum retry count  ", log.Any("retryTimes", h.retryTimes), log.Error(syncResult.Err))
					continue
				}
				syncResult.Extra["times"] = times + 1
				l.Error("http send  err ,start resend", log.Error(syncResult.Err))
				h.cli.SyncSendUrl(h.method, h.path, bytes.NewReader(body), h.syncResult, syncResult.Extra, h.header)
			} else {
				l.Info("send success")
			}
			if syncResult.Response != nil && syncResult.Response.Body != nil {
				err := syncResult.Response.Body.Close()
				if err != nil {
					l.Error("http response body close error", log.Error(err))
				}
			}
		default:
		}
	}
}

func InitLogInfo(syncResult *http.SyncResults) *log.Logger {
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

func (h *httpClient) GetLinkID() string {
	return h.id
}

func (h *httpClient) GetFailMsg() chan []byte {
	return h.failMsg
}

func (h *httpClient) SetFailMsg(fallMsg chan []byte) {
	h.failMsg = fallMsg
}
