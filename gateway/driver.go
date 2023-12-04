package gateway

import (
	"context"
	"sync"
	"time"

	plugin "github.com/baetyl/baetyl-gateway-sdk/sdk/golang"
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/json"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/spec/v1"
	"github.com/baetyl/baetyl-go/v2/utils"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/models"
	"github.com/baetyl/baetyl-gateway/north"
	"github.com/baetyl/baetyl-gateway/south"
	"github.com/baetyl/baetyl-gateway/south/mqtt"
)

const DiskCache = "disk"

type driverControl struct {
	uplinkCh map[string]chan *models.MsgChan
	ctx      context.Context
	cancel   context.CancelFunc
}

type driverEngine struct {
	ctx                  dm.Context
	cfg                  config.Config
	northLink            *north.ClientNorth
	mqtt                 *mqtt.Client
	rpt                  *ReportImpl               // 驱动数据及状态上报接口
	downlinkCh           chan *models.MsgChan      // 南向消息队列，软网关->驱动
	driverCtl            map[string]*driverControl // 北向消息队列，驱动->软网关，按照驱动区分，每个设备一个独立的channel
	tomb                 utils.Tomb
	log                  *log.Logger
	propertyLock         sync.RWMutex
	lastReportProperties map[string]map[string]dm.ReportProperty
	statusMutex          sync.RWMutex
	deviceStatus         map[string]int
	entryCtl             context.Context    // 软网关总上下文
	cancel               context.CancelFunc // 调用cancel方法，退出所有处理协程
}

func NewDriverEngine(ctx dm.Context, cfg config.Config) (models.DriverEngine, error) {
	// 创建mqtt client
	mqttCli, err := mqtt.NewMqtt(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 判断是否用系统应用 如果不存在北向使用系统默认mqtt
	if len(cfg.Uplink.HTTPConfig) == 0 && len(cfg.Uplink.MQTTConfig) == 0 && len(cfg.Uplink.WebsocketConfig) == 0 {
		// 获取mqtt 默认config
		defaultConfig := cfg.MqttConfig
		if defaultConfig.Address == "" {
			defaultConfig, err = ctx.NewSystemBrokerClientConfig()
			if err != nil {
				return nil, err
			}
		}
		// 系统mqtt配置增加client 配置不同client id
		defaultConfig.ClientID = utils.RandString(10) + defaultConfig.ClientID
		cfg.Uplink.MQTTConfig = []config.NorthMQTTConfig{
			{
				ClientConfig: defaultConfig,
				RetryTimes:   3,
			},
		}
	}
	//  判断是否用系统应用 如果配置mqtt 配置地址为baetyl-system 使用系统默认mqtt
	if cfg.Uplink.MQTTConfig != nil {
		for i := range cfg.Uplink.MQTTConfig {
			// 云端下发baetyl-system 代表系统应用 读取系统配置
			if cfg.Uplink.MQTTConfig[i].Address == north.MQTTSystemConfig {
				defaultConfig := cfg.MqttConfig
				if defaultConfig.Address == "" {
					defaultConfig, err = ctx.NewSystemBrokerClientConfig()
					if err != nil {
						return nil, err
					}
				}
				// 系统mqtt配置增加client 配置不同client id
				defaultConfig.ClientID = utils.RandString(10) + defaultConfig.ClientID
				cfg.Uplink.MQTTConfig[i].ClientConfig = defaultConfig
				cfg.Uplink.MQTTConfig[i].RetryTimes = 3
			}
		}
	}

	northClient, err := north.CreateNorthClient(cfg, ctx.NodeName())
	if err != nil {
		return nil, err
	}
	downlinkCh := make(chan *models.MsgChan, 1024)
	driverCtl := make(map[string]*driverControl)
	entryCtl, cancel := context.WithCancel(context.Background())
	var deviceArray []string
	for _, driver := range cfg.Plugin.Drivers {
		uplinkCh := make(map[string]chan *models.MsgChan)
		dCtx, dCtl := context.WithCancel(entryCtl)

		for _, device := range ctx.GetAllDevices(driver.Name) {
			deviceArray = append(deviceArray, device.Name)
			uplinkCh[device.Name] = make(chan *models.MsgChan, 1024)
		}
		driverCtl[driver.Name] = &driverControl{
			uplinkCh: uplinkCh,
			ctx:      dCtx,
			cancel:   dCtl,
		}
	}

	if cfg.MessageCacheConfig.Enable && cfg.MessageCacheConfig.CacheType == DiskCache {
		go northClient.ClearUselessData(deviceArray)
	}

	return &driverEngine{
		downlinkCh:           downlinkCh,
		driverCtl:            driverCtl,
		mqtt:                 mqttCli,
		northLink:            northClient,
		rpt:                  NewReport(driverCtl),
		ctx:                  ctx,
		entryCtl:             entryCtl,
		cancel:               cancel,
		cfg:                  cfg,
		log:                  log.L().With(log.Any("gateway", "driverEngine")),
		lastReportProperties: make(map[string]map[string]dm.ReportProperty),
		deviceStatus:         make(map[string]int),
	}, nil
}

func (d *driverEngine) UpdateNorthLink(linkConfig *config.UplinkConfig) error {
	northClient, err := north.CreateNorthClient(config.Config{Uplink: *linkConfig}, d.ctx.NodeName())
	if err != nil {
		return err
	}
	d.northLink = northClient
	return nil
}

func (d *driverEngine) Start() error {
	// 启动驱动
	err := d.startDriver()
	if err != nil {
		d.log.Error("failed to start driver")
		return err
	}
	// 南向消息订阅，放入消息队列中
	err = d.mqtt.Start(d.downlinkCh)
	if err != nil {
		d.log.Error("failed tp start mqtt")
		return err
	}
	// 插件消息处理分设备
	for _, dCtl := range d.driverCtl {
		for _, ch := range dCtl.uplinkCh {
			go d.processUplinkMsg(dCtl.ctx, ch)
		}
	}
	go d.processDownlinkMsg()
	return nil
}

func (d *driverEngine) Close() {
	d.log.Info("gateway closing")
	d.cancel()
	var err error
	if d.mqtt != nil {
		err = d.mqtt.Close()
		if err != nil {
			d.log.Error("failed to close mqtt client", log.Any("err", err))
		}
	}
	for _, driver := range d.cfg.Plugin.Drivers {
		plg, err := plugin.GetPlugin(driver.Name)
		if err != nil {
			return
		}
		_, err = plg.Stop(&plugin.Request{})
		if err != nil {
			return
		}
		err = plugin.ClosePlugin(driver.Name)
		if err != nil {
			d.log.Error("failed to close driver client", log.Any("plugin", driver.Name), log.Any("err", err))
			continue
		}
	}
}

func (d *driverEngine) startDriver() error {
	for _, driver := range d.cfg.Plugin.Drivers {
		// 插件注册与连接建立
		c, err := plugin.RegisterPlugin(driver)
		if err != nil {
			d.log.Error("failed to register driver", log.Any("plugin", driver.Name), log.Any("error", err))
			return err
		}
		ctx := &comctx.Context{
			Context: nil,
			Logger:  d.log,
		}
		// 插件运行
		err = d.StartDriver(ctx, c, driver.Name, driver.ConfigPath)
		if err != nil {
			d.log.Error("failed to start driver", log.Any("plugin", driver.Name), log.Any("error", err))
			return errors.Trace(err)
		}
	}
	return nil
}

// 北向消息主处理
func (d *driverEngine) processUplinkMsg(ctx context.Context, ch chan *models.MsgChan) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			bytes, err := json.Marshal(msg.Msg)
			if err != nil {
				msg.Ctx.Logger.Error("failed to marshal msg")
			}
			msgStr := string(bytes)
			msg.Ctx.Logger.Debug("receive uplink message", log.Any("message", msgStr))
			switch msg.Msg.Kind {
			case v1.MessageDeviceDesire:
				err = d.processReportMsg(msg.Ctx, msg.Msg, false)
				if err != nil {
					msg.Ctx.Logger.Error("failed to process device property message", log.Any("message", msgStr), log.Error(err))
				}
				msg.Ctx.Logger.Debug("process uplink message successfully", log.Any("message", msgStr))
			case v1.MessageDeviceReport:
				err = d.processReportMsg(msg.Ctx, msg.Msg, true)
				if err != nil {
					msg.Ctx.Logger.Error("failed to process device report message", log.Any("message", msgStr), log.Error(err))
				}
				msg.Ctx.Logger.Debug("process uplink message successfully", log.Any("message", msgStr))
			case v1.MessageDeviceLifecycleReport:
				err = d.processDownLifecycleReportMsg(msg.Ctx, msg.Msg)
				if err != nil {
					msg.Ctx.Logger.Error("failed to process device lifecycle report message", log.Any("message", msgStr), log.Error(err))
				}
				msg.Ctx.Logger.Debug("process uplink message successfully", log.Any("message", msgStr))
			default:
				msg.Ctx.Logger.Error("message kind unsupported", log.Any("message", msgStr))
			}

		}
	}
}

func (d *driverEngine) processDownGetMsg(ctx *comctx.Context, msg *v1.Message) error {
	return d.processReportMsg(ctx, msg, false)
}

func (d *driverEngine) processReportMsg(ctx *comctx.Context, msg *v1.Message, report bool) error {
	var driverName, devName string
	var ok bool
	temp := make(map[string]any)
	r := v1.Report{}

	if driverName, ok = msg.Metadata[dm.KeyDriverName]; !ok {
		return errors.New("failed to get driverName in report msg")
	}
	if devName, ok = msg.Metadata[dm.KeyDeviceName]; !ok {
		return errors.New("failed to get deviceName in report msg")
	}

	if err := msg.Content.Unmarshal(&temp); err != nil {
		return err
	}

	devInfo, err := d.ctx.GetDevice(driverName, devName)
	if err != nil {
		return err
	}

	// 物模型点位映射
	accessTemplate, err := d.ctx.GetAccessTemplates(driverName, devInfo.AccessTemplate)
	if err != nil {
		return err
	}
	for _, model := range accessTemplate.Mappings {
		// 无映射直接跳过
		if model.Type == dm.MappingNone {
			continue
		}
		args := make(map[string]any)
		params, parErr := dm.ParseExpression(model.Expression)
		if parErr != nil {
			return parErr
		}
		for _, param := range params {
			id := param[1:]
			mappingName, adpErr := dm.GetMappingName(id, accessTemplate)
			if adpErr != nil {
				return adpErr
			}
			args[param] = temp[mappingName]
		}
		modelValue, exeErr := dm.ExecExpressionWithPrecision(model.Expression, args, model.Type, model.Precision)
		if exeErr != nil {
			return exeErr
		}
		if modelValue == nil {
			continue
		}
		r[model.Attribute] = modelValue
	}

	ctx.Logger.Info("prepare to send msg", log.Any("devInfo", devInfo), log.Any("message", r))
	if report {
		return d.reportDevicePropertiesWithFilter(ctx, driverName, devInfo, r, devInfo.DeviceTopic.Report.Topic, report)
	}
	return d.reportDevicePropertiesWithFilter(ctx, driverName, devInfo, r, devInfo.DeviceTopic.Get.Topic, report)
}

func (d *driverEngine) processDownLifecycleReportMsg(ctx *comctx.Context, msg *v1.Message) error {
	var driverName, devName string
	var ok bool
	var r bool

	if driverName, ok = msg.Metadata[dm.KeyDriverName]; !ok {
		return errors.New("failed to get driverName in state msg")
	}
	if devName, ok = msg.Metadata[dm.KeyDeviceName]; !ok {
		return errors.New("failed to get deviceName in state msg")
	}

	if err := msg.Content.Unmarshal(&r); err != nil {
		return err
	}

	devInfo, err := d.ctx.GetDevice(driverName, devName)
	if err != nil {
		return err
	}

	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	if r {
		d.deviceStatus[devInfo.Name] = dm.DeviceOnline
		return d.mqtt.Online(ctx, d.ctx.NodeName(), devInfo)
		//d.northLink.Online(ctx, d.ctx.NodeName(), devInfo)
	}
	d.deviceStatus[devInfo.Name] = dm.DeviceOffline
	return d.mqtt.Offline(ctx, d.ctx.NodeName(), devInfo)
	//d.northLink.Offline(ctx, d.ctx.NodeName(), devInfo)
}

// 南向消息主处理
func (d *driverEngine) processDownlinkMsg() {
	for {
		select {
		case <-d.entryCtl.Done():
			return
		case msg := <-d.downlinkCh:
			bytes, err := json.Marshal(msg)
			if err != nil {
				d.log.Error("failed to marshal msg")
			}
			msgStr := string(bytes)
			msg.Ctx.Logger.Debug("receive downlink message", log.Any("message", msgStr))
			switch msg.Msg.Kind {
			case v1.MessageDeviceDelta:
				if err = d.ProcessDelta(msg.Ctx, msg.Msg); err != nil {
					msg.Ctx.Logger.Error("failed to process delta message", log.Error(err))
				} else {
					msg.Ctx.Logger.Info("process delta message successfully")
				}
			case v1.MessageDeviceEvent:
				if err = d.ProcessEvent(msg.Ctx, msg.Msg); err != nil {
					msg.Ctx.Logger.Error("failed to process event message", log.Error(err))
				} else {
					msg.Ctx.Logger.Info("process event message successfully")
				}
			case v1.MessageResponse:
				if err = d.processResponse(msg.Ctx, msg.Msg); err != nil {
					msg.Ctx.Logger.Error("failed to process response message", log.Error(err))
				} else {
					msg.Ctx.Logger.Info("process response message successfully")
				}
			case v1.MessageDevicePropertyGet:
				if err = d.ProcessPropertyGet(msg.Ctx, msg.Msg); err != nil {
					msg.Ctx.Logger.Error("failed to process property get message", log.Error(err))
				} else {
					msg.Ctx.Logger.Info("process property get message successfully")
				}
			default:
				msg.Ctx.Logger.Error("device message type not supported yet")
			}
		}
	}
}

func (d *driverEngine) ProcessDelta(ctx *comctx.Context, msg *v1.Message) error {
	var driverName, deviceName string
	var ok bool

	if deviceName, ok = msg.Metadata[south.KeyDevice]; !ok {
		return errors.New("device name not found in metadata")
	}

	if driverName = d.ctx.GetDriverNameByDevice(deviceName); driverName == "" {
		return errors.New("driver name not exist")
	}

	var blinkContent south.BlinkContent
	if err := msg.Content.ExactUnmarshal(&blinkContent); err != nil {
		return err
	}
	delta, ok := blinkContent.Blink.Properties.(map[string]any)
	if !ok {
		return dm.ErrInvalidDelta
	}
	dev, err := d.ctx.GetDevice(driverName, deviceName)
	if err != nil {
		return err
	}
	delta, err = d.ctx.ParsePropertyValues(driverName, dev, delta)
	if err != nil {
		return err
	}

	accessTemplate, err := d.ctx.GetAccessTemplates(driverName, dev.AccessTemplate)
	if err != nil {
		ctx.Logger.Warn("get access template err", log.Any("device", dev.Name))
		return err
	}

	var props []config.DeviceProperty
	for key, val := range delta {
		var prop config.DeviceProperty
		id, adpErr := dm.GetConfigIDByModelName(key, accessTemplate)
		if id == "" || adpErr != nil {
			ctx.Logger.Warn("prop not exist", log.Any("name", key), log.Any("err", err))
			continue
		}
		propName, adpErr := dm.GetMappingName(id, accessTemplate)
		if adpErr != nil {
			ctx.Logger.Warn("prop name not exist", log.Any("id", id), log.Any("err", err))
			continue
		}
		propVal, adpErr := dm.GetPropValueByModelName(key, val, accessTemplate)
		if adpErr != nil {
			ctx.Logger.Warn("get prop value err", log.Any("name", propName), log.Any("err", err))
			continue
		}
		prop.PropName = propName
		prop.PropVal = propVal
		props = append(props, prop)
	}

	plg, err := plugin.GetPlugin(driverName)
	if err != nil {
		return err
	}

	deltaMsg := v1.Message{}
	deltaMsg.Kind = v1.MessageDeviceDelta
	deltaMsg.Metadata = make(map[string]string)
	deltaMsg.Metadata[dm.KeyDriverName] = driverName
	deltaMsg.Metadata[dm.KeyDeviceName] = deviceName
	deltaMsg.Content = v1.LazyValue{Value: props}

	msgData, err := json.Marshal(deltaMsg)
	if err != nil {
		return err
	}
	_, err = plg.Set(&plugin.Request{Req: string(msgData), RequestID: ctx.GetRequestID()})
	if err != nil {
		return err
	}
	return nil
}

func (d *driverEngine) ProcessEvent(ctx *comctx.Context, msg *v1.Message) error {
	var driverName, deviceName string
	var ok bool

	if deviceName, ok = msg.Metadata[south.KeyDevice]; !ok {
		return errors.New("device name not found in metadata")
	}

	if driverName = d.ctx.GetDriverNameByDevice(deviceName); driverName == "" {
		return errors.New("driver name not exist")
	}

	var event dm.Event
	if err := msg.Content.Unmarshal(&event); err != nil {
		return err
	}

	plg, err := plugin.GetPlugin(driverName)
	if err != nil {
		return err
	}

	eventMsg := v1.Message{}
	eventMsg.Kind = v1.MessageDeviceEvent
	eventMsg.Metadata = make(map[string]string)
	eventMsg.Metadata[dm.KeyDriverName] = driverName
	eventMsg.Metadata[dm.KeyDeviceName] = deviceName
	eventMsg.Content = v1.LazyValue{Value: event}

	msgData, err := json.Marshal(eventMsg)
	if err != nil {
		return err
	}
	_, err = plg.Get(&plugin.Request{Req: string(msgData), RequestID: ctx.GetRequestID()})
	if err != nil {
		return err
	}
	return nil
}

func (d *driverEngine) processResponse(c *comctx.Context, msg *v1.Message) error {
	return nil
}

func (d *driverEngine) ProcessPropertyGet(ctx *comctx.Context, msg *v1.Message) error {
	var driverName, deviceName string
	var ok bool

	if deviceName, ok = msg.Metadata[south.KeyDevice]; !ok {
		return errors.New("device name not found in metadata")
	}

	if driverName = d.ctx.GetDriverNameByDevice(deviceName); driverName == "" {
		return errors.New("driver name not exist")
	}

	var blinkContent south.BlinkContent
	if err := msg.Content.Unmarshal(&blinkContent); err != nil {
		return err
	}
	properties, err := dm.ParsePropertyKeys(blinkContent.Blink.Properties)
	if err != nil {
		return err
	}

	plg, err := plugin.GetPlugin(driverName)
	if err != nil {
		return err
	}

	propGetMsg := v1.Message{}
	propGetMsg.Kind = v1.MessageDevicePropertyGet
	propGetMsg.Metadata = make(map[string]string)
	propGetMsg.Metadata[dm.KeyDriverName] = driverName
	propGetMsg.Metadata[dm.KeyDeviceName] = deviceName
	propGetMsg.Content = v1.LazyValue{Value: properties}

	msgData, err := json.Marshal(propGetMsg)
	if err != nil {
		return err
	}
	_, err = plg.Get(&plugin.Request{Req: string(msgData), RequestID: ctx.GetRequestID()})
	if err != nil {
		return err
	}
	return nil
}

func (d *driverEngine) GetAllConfigs(c *comctx.Context, drivers []string) *models.AllPluginConfigs {
	result := &models.AllPluginConfigs{
		DeviceModels:   make(map[string][]dm.DeviceProperty),
		AccessTemplate: make(map[string]models.AccessTemplate),
		Devices:        make(map[string]models.DeviceInfo),
	}
	for _, driver := range drivers {
		devs := d.ctx.GetAllDevices(driver)
		for _, dev := range devs {
			result.Devices[dev.Name] = models.DeviceInfo{
				DeviceInfo: dev,
				Driver:     d.ctx.GetDriverNameByDevice(dev.Name),
			}
			deviceModel, err := d.ctx.GetDeviceModel(driver, &dev)
			if err != nil {
				continue
			}
			if _, ok := result.DeviceModels[dev.DeviceModel]; !ok {
				result.DeviceModels[dev.DeviceModel] = deviceModel
			}
			accessTemplate, err := d.ctx.GetAccessTemplates(driver, dev.AccessTemplate)
			if err != nil {
				continue
			}
			if _, ok := result.AccessTemplate[dev.AccessTemplate]; !ok {
				result.AccessTemplate[dev.AccessTemplate] = models.AccessTemplate{
					AccessTemplate: *accessTemplate,
					DeviceModel:    dev.DeviceModel,
				}
			}
		}
	}
	return result
}

func (d *driverEngine) GetDeviceShadow(c *comctx.Context, deviceName string) (map[string]dm.ReportProperty, error) {
	d.propertyLock.RLock()
	shadow, ok := d.lastReportProperties[deviceName]
	d.propertyLock.RUnlock()
	if !ok {
		return nil, errors.New("Device shadow not found")
	}
	return shadow, nil
}

func (d *driverEngine) GetPluginConfigPath(ctx *comctx.Context, driverName string) string {
	for _, cfg := range d.cfg.Plugin.Drivers {
		if cfg.Name == driverName {
			return cfg.ConfigPath
		}
	}
	return ""
}

func (d *driverEngine) StartDriver(ctx *comctx.Context, client *plugin.Client, driverName string, path string) error {
	// 传递上报接口至插件侧供其调用
	res, err := client.Setup(&plugin.BackendConfig{ReportSvc: d.rpt, DriverName: driverName})
	if err != nil {
		d.log.Error("failed to setup driver", log.Any("plugin", driverName), log.Any("error", err))
		return err
	}
	d.log.Info(res.Data)

	// 插件配置文件路径
	res, err = client.SetConfig(&plugin.Request{Req: path})
	if err != nil {
		d.log.Error("failed to set driver config path", log.Any("plugin", driverName), log.Any("error", err))
		return err
	}
	d.log.Info(res.Data)

	// 插件运行
	_, err = client.Start(&plugin.Request{})
	if err != nil {
		d.log.Error("failed to start driver", log.Any("plugin", driverName), log.Any("error", err))
		return err
	}
	return nil
}

func (d *driverEngine) ContextReload(driverName string, path string) error {
	err := d.ctx.LoadDriverConfig(path, driverName)
	d.driverCtl[driverName].cancel()
	uplinkCh := make(map[string]chan *models.MsgChan)
	dCtx, dCtl := context.WithCancel(d.entryCtl)
	for _, device := range d.ctx.GetAllDevices(driverName) {
		ch := make(chan *models.MsgChan, 1024)
		uplinkCh[device.Name] = ch
		go d.processUplinkMsg(dCtx, ch)
	}
	d.driverCtl[driverName] = &driverControl{
		uplinkCh: uplinkCh,
		ctx:      dCtx,
		cancel:   dCtl,
	}
	return err
}

func (d *driverEngine) GetDevicesStatusByDriver(_ *comctx.Context, driverName string) map[string]int {
	result := make(map[string]int)
	devs := d.ctx.GetAllDevices(driverName)
	d.propertyLock.RLock()
	defer d.propertyLock.RUnlock()
	for _, dev := range devs {
		status, ok := d.deviceStatus[dev.Name]
		if !ok {
			result[dev.Name] = dm.DeviceOffline
			continue
		}
		result[dev.Name] = status
	}
	return result
}

func (d *driverEngine) reportDevicePropertiesWithFilter(ctx *comctx.Context, driverName string,
	device *dm.DeviceInfo, report v1.Report, topic string, isReport bool) error {
	filterReport := make(map[string]any)
	propMapping := make(map[string]dm.ModelMapping)

	// get device templates properties
	accessTemplates, err := d.ctx.GetAccessTemplates(driverName, device.AccessTemplate)
	if err != nil {
		return err
	}
	// generate propMapping, key=propName, value=ModelMapping
	for _, mapping := range accessTemplates.Mappings {
		propMapping[mapping.Attribute] = mapping
	}
	d.propertyLock.Lock()
	d.lastReportProperties[device.Name] = make(map[string]dm.ReportProperty)
	// filter report
	for key, value := range report {
		mapping, isExist := propMapping[key]
		if !isExist {
			continue
		}
		// first report or out of silentWin
		if _, ok := d.lastReportProperties[device.Name][key]; !ok ||
			time.Now().Sub(d.lastReportProperties[device.Name][key].Time) > time.Duration(mapping.SilentWin)*time.Second {
			filterReport[key] = value
			d.lastReportProperties[device.Name][key] = dm.ReportProperty{Time: time.Now(), Value: value}
		}
		// greater than deviation
		if isCalculable(value) {
			parseVal, pErr := dm.ParseValueToFloat64(value)
			if pErr != nil {
				continue
			}
			lastVal, pErr := dm.ParseValueToFloat64(d.lastReportProperties[device.Name][key].Value)
			if pErr != nil {
				continue
			}
			if parseVal-lastVal >= lastVal*mapping.Deviation/100 {
				filterReport[key] = value
				d.lastReportProperties[device.Name][key] = dm.ReportProperty{Time: time.Now(), Value: value}
			}
		}
	}
	d.propertyLock.Unlock()
	if isReport {
		return d.northLink.ReportDeviceProperties(ctx, d.ctx.NodeName(), device, filterReport, topic)
	}
	return d.mqtt.ReportDeviceProperties(ctx, d.ctx.NodeName(), device, filterReport, topic)
}

func isCalculable(v any) bool {
	switch v.(type) {
	case int, int16, int32, int64, float32, float64:
		return true
	default:
		return false
	}
}
