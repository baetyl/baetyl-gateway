package north

import (
	"strings"
	"time"

	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	"github.com/baetyl/baetyl-go/v2/plugin"
	"github.com/baetyl/baetyl-go/v2/spec/v1"
	"github.com/jpillora/backoff"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/north/dataprocessing"
	"github.com/baetyl/baetyl-gateway/north/dataprocessing/cache"
	"github.com/baetyl/baetyl-gateway/north/link"
	linkhttp "github.com/baetyl/baetyl-gateway/north/link/http"
	linkmqtt "github.com/baetyl/baetyl-gateway/north/link/mqtt"
	linkws "github.com/baetyl/baetyl-gateway/north/link/websocket"
)

const MQTTSystemConfig = "baetyl-system"

type ClientNorth struct {
	link                []link.Link
	processFunc         []string
	msg                 dm.Msg
	cache               cache.Cache
	sendMessageInterval time.Duration
	maxInterval         time.Duration
	log                 *log.Logger
}

func CreateNorthClient(cfg config.Config, bucketName string) (*ClientNorth, error) {
	var northLink []link.Link
	///linkCacheMap := make(map[string]string, 3)
	// 编写新的协议要求 newClient 要保证初始化配置信息 配置错误直接不在连接
	// 如果newClient 有链接信息要 确保不会是因为链接失败返回错误  内部要实现对连接的重连机制
	// TODO 三种创建不同的链接合并成一个struct,通过插件的方式实现创建链接
	for i := range cfg.Uplink.HTTPConfig {
		httpConfig := cfg.Uplink.HTTPConfig[i]
		tempLink, err := linkhttp.NewClient(httpConfig)
		if err != nil {
			log.L().Error("set http Link error", log.Any("httpmsg", httpConfig), log.Any("error", err))
			continue
		}
		northLink = append(northLink, tempLink)
	}

	for i := range cfg.Uplink.MQTTConfig {
		mqttConfig := cfg.Uplink.MQTTConfig[i]
		tempLink, err := linkmqtt.NewClient(mqttConfig)
		if err != nil {
			log.L().Error("set mqtt Link error", log.Any("mqttmsg", mqttConfig), log.Any("error", err))
			continue
		}
		northLink = append(northLink, tempLink)
	}

	for i := range cfg.Uplink.WebsocketConfig {
		wsConfig := cfg.Uplink.WebsocketConfig[i]
		tempLink, err := linkws.NewClient(wsConfig)
		if err != nil {
			log.L().Error("set websocket Link error", log.Any("wsmsg", wsConfig), log.Any("error", err))
			continue
		}
		northLink = append(northLink, tempLink)
	}
	clientNorth := &ClientNorth{
		link:                northLink,
		processFunc:         cfg.ConvertFunc,
		msg:                 dm.InitMsg(dm.Blink),
		sendMessageInterval: cfg.SendMessageInterval,
		maxInterval:         cfg.MaxInterval,
		log:                 log.L().With(log.Any("client", "clientNorth")),
	}

	if cfg.MessageCacheConfig.Enable {
		for _, l := range northLink {
			failMsg := make(chan []byte, cfg.MessageCacheConfig.FailChanSize)
			l.SetFailMsg(failMsg)
		}

		msgCache, err := plugin.GetPlugin(cfg.MessageCacheConfig.CacheType)
		if err != nil {
			log.L().Error("failed to create message cache")
		}
		clientNorth.cache = msgCache.(cache.Cache)
		for _, l := range northLink {
			cacheBucket := bucketName + l.GetLinkID()
			go CacheMsg(clientNorth.cache, cacheBucket, l.GetFailMsg())
		}
		// enable sending cache data
		go clientNorth.EnableCacheMsgSend(bucketName)
	}
	return clientNorth, nil
}

func (n *ClientNorth) MsgSend(ctx *comctx.Context, msg *link.SendMessage) error {
	err := n.DataConvertFuncRun(msg)
	if err != nil {
		return errors.Trace(err)
	}
	for i := range n.link {
		l := n.link[i]
		err := l.Send(ctx, msg)
		if err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}

func (n *ClientNorth) DataConvertFuncRun(msg *link.SendMessage) error {
	for i := range n.processFunc {
		err := dataprocessing.DataProcess.RunFunc(n.processFunc[i], msg)
		if err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}

func (n *ClientNorth) ReportDeviceProperties(ctx *comctx.Context, node string, info *dm.DeviceInfo, report v1.Report, topic string) error {
	msg := &link.SendMessage{
		Message: &v1.Message{
			Kind:     v1.MessageDeviceReport,
			Metadata: genMetadata(node, info),
			Content:  n.msg.GenPropertyReportData(report),
		},
		SendMessage: []byte{},
		MqttInfo: link.MqttInfo{
			Topic: topic,
			Qos:   mqtt.QOS(info.DeviceTopic.Report.QOS),
		},
		Times: 1,
	}
	return n.MsgSend(ctx, msg)
}

func (n *ClientNorth) ReportDeviceEvents(ctx *comctx.Context, node string, info *dm.DeviceInfo, report v1.EventReport) error {
	msg := &link.SendMessage{
		Message: &v1.Message{
			Kind:     v1.MessageDeviceEventReport,
			Metadata: genMetadata(node, info),
			Content:  n.msg.GenEventReportData(report),
		},
		SendMessage: []byte{},
		MqttInfo: link.MqttInfo{
			Topic: info.DeviceTopic.EventReport.Topic,
			Qos:   mqtt.QOS(info.DeviceTopic.EventReport.QOS),
		},
		Times: 1,
	}
	return n.MsgSend(ctx, msg)
}

func (n *ClientNorth) Online(ctx *comctx.Context, node string, info *dm.DeviceInfo) error {
	msg := &link.SendMessage{
		Message: &v1.Message{
			Kind:     v1.MessageDeviceLifecycleReport,
			Metadata: genMetadata(node, info),
			Content:  n.msg.GenLifecycleReportData(true),
		},
		SendMessage: []byte{},
		MqttInfo: link.MqttInfo{
			Topic: info.DeviceTopic.LifecycleReport.Topic,
			Qos:   mqtt.QOS(info.DeviceTopic.LifecycleReport.QOS),
		},
		Times: 1,
	}
	return n.MsgSend(ctx, msg)
}

func (n *ClientNorth) Offline(ctx *comctx.Context, node string, info *dm.DeviceInfo) error {
	msg := &link.SendMessage{
		Message: &v1.Message{
			Kind:     v1.MessageDeviceLifecycleReport,
			Metadata: genMetadata(node, info),
			Content:  n.msg.GenLifecycleReportData(false),
		},
		SendMessage: []byte{},
		MqttInfo: link.MqttInfo{
			Topic: info.DeviceTopic.LifecycleReport.Topic,
			Qos:   mqtt.QOS(info.DeviceTopic.LifecycleReport.QOS),
		},
		Times: 1,
	}
	return n.MsgSend(ctx, msg)
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

func CacheMsg(cache cache.Cache, bucketName string, failMsg chan []byte) {
	for {
		select {
		case data := <-failMsg:
			err := cache.StoreCacheMsg(bucketName, data)
			if err != nil {
				log.L().Error("failed to store this message", log.Any("msg:", string(data)))
			}
		}
	}
}

func (n *ClientNorth) EnableCacheMsgSend(bucket string) {
	for _, li := range n.link {
		go n.cacheMsgSending(li.GetLinkID()+bucket, li)
	}
}

func (n *ClientNorth) cacheMsgSending(name string, northLink link.Link) {
	var next time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()
	bf := backoff.Backoff{
		Min:    time.Second,
		Max:    30 * time.Second,
		Factor: 2,
	}

	for {
		if !next.IsZero() {
			timer.Reset(next.Sub(time.Now()))
		}
		select {
		case <-timer.C:
			var needToDelete [][]byte
			kvMap, err := n.cache.GetCacheMsg(name)
			if err != nil {
				next = time.Now().Add(bf.Duration())
				n.log.Error("fail to get msg in bucket", log.Any("err", err.Error()))
			}
			if len(needToDelete) == 0 {
				next = time.Now().Add(bf.Duration())
			}
			for k, v := range kvMap {
				time.Sleep(n.sendMessageInterval)
				ctx := comctx.NewTraceContext("")
				err := northLink.SyncSend(ctx, v)
				if err != nil {
					next = time.Now().Add(bf.Duration())
					n.log.Error("this msg send error")
					break
				} else {
					bf.Reset()
					needToDelete = append(needToDelete, []byte(k))
				}
			}
			if len(needToDelete) == 0 {
				continue
			}
			err = n.cache.DeleteCacheMsg(name, needToDelete)
			if err != nil {
				n.log.Error("fail to delete msg", log.Any("err", err.Error()))
			}
		}
	}
}

func (n *ClientNorth) ClearUselessData(deviceArray []string) {
	dataMap, err := n.cache.GetAllCacheMsg()
	if err != nil {
		n.log.Error("failed to clean up useless data from cache")
	}
	needDeleteMap := make(map[string][][]byte)
	for bucket, data := range dataMap {
		var keList [][]byte
		for _, da := range data {
			if !dataScreening(bucket, da, deviceArray) {
				keList = append(keList, da)
			}
		}
		needDeleteMap[bucket] = keList
	}
	err = n.cache.DeleteTargetMsg(needDeleteMap)
	if err != nil {
		n.log.Error("failed to clear cached data for target")
	}
}

func dataScreening(bucket string, data []byte, deviceArray []string) bool {
	if strings.Contains(bucket, "mqtt") {
		sendMsg := link.SendMessage{}
		err := json.Unmarshal(data, &sendMsg)
		if err != nil {
			return true
		}
		data = sendMsg.SendMessage
	}
	msg := v1.Message{}
	err := json.Unmarshal(data, &msg)
	if err != nil {
		return true
	}
	for _, s := range deviceArray {
		if s == msg.Metadata["device"] {
			return true
		}
	}
	return false
}
