package api

import (
	"errors"
	"os"

	plugin "github.com/baetyl/baetyl-gateway-sdk/sdk/golang"
	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"gopkg.in/yaml.v2"

	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/models"
)

type PluginStatus struct {
	Name   string         `json:"name,omitempty"`
	Enable bool           `json:"enable,omitempty"`
	On     bool           `json:"on,omitempty"`
	Status map[string]int `json:"status,omitempty"`
}

type PluginAPI interface {
	CreateAndActivatePlugin(ctx *comctx.Context) (any, error)
	ActivatePlugin(ctx *comctx.Context) (any, error)
	ClosePlugin(ctx *comctx.Context) (any, error)
	RestartPlugin(ctx *comctx.Context) (any, error)
	CheckPluginStatus(ctx *comctx.Context) (any, error)
	CheckAllPluginStatus(ctx *comctx.Context) (any, error)
	GetAllConfigs(ctx *comctx.Context) (any, error)
	UpdateNorthLink(c *comctx.Context) (any, error)
}

type pluginImpl struct {
	driver models.DriverEngine
}

func NewPluginAPI(driver models.DriverEngine) PluginAPI {
	return &pluginImpl{
		driver: driver,
	}
}

// CreateAndActivatePlugin 注册并激活插件
func (p *pluginImpl) CreateAndActivatePlugin(ctx *comctx.Context) (any, error) {
	plugin.PluginLock.Lock()
	defer plugin.PluginLock.Unlock()

	name := ctx.Param("name")
	path := ctx.Param("path")
	if c, ok := plugin.PluginFactories[name]; ok {
		return c, nil
	}
	c, err := plugin.NewClient(name, path)
	if err != nil {
		return nil, err
	}
	plugin.PluginFactories[name] = c

	return c, nil
}

// ActivatePlugin 激活插件，需先注册才可激活
func (p *pluginImpl) ActivatePlugin(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	plugin.PluginLock.Lock()
	client := plugin.PluginFactories[name]
	plugin.PluginLock.Unlock()

	if client == nil {
		return nil, errors.New("plugin " + name + " not registered")
	}
	return nil, client.Open()
}

// ClosePlugin 去激活插件，停止插件进程
func (p *pluginImpl) ClosePlugin(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	plugin.PluginLock.Lock()
	client := plugin.PluginFactories[name]
	plugin.PluginLock.Unlock()

	if client == nil {
		return nil, errors.New("plugin " + name + " no register")
	}
	return name, client.Disable()
}

func (p *pluginImpl) RestartPlugin(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	configs := new(models.PluginConfig)
	if err := ctx.LoadBody(configs); err != nil {
		return nil, err
	}
	plugin.PluginLock.Lock()
	defer plugin.PluginLock.Unlock()
	client, ok := plugin.PluginFactories[name]
	if !ok {
		return nil, errors.New("plugin " + name + " no register")
	}
	err := client.Disable()
	if err != nil {
		return nil, err
	}
	path := p.driver.GetPluginConfigPath(ctx, name)
	if path == "" {
		return nil, errors.New("plugin " + name + " no register")
	}

	deviceModels, err := yaml.Marshal(configs.Models)
	if err != nil {
		return nil, err
	}
	accessTemplates, err := yaml.Marshal(configs.AccessTemplate)
	if err != nil {
		return nil, err
	}
	subDevices, err := yaml.Marshal(configs.SubDevices)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile(path+"/"+dm.DefaultDeviceModelConf, deviceModels, 0777)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(path+"/"+dm.DefaultAccessTemplateConf, accessTemplates, 0777)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(path+"/"+dm.DefaultSubDeviceConf, subDevices, 0777)
	if err != nil {
		return nil, err
	}

	err = client.Open()
	if err != nil {
		return nil, err
	}

	err = p.driver.ContextReload(name, path)
	if err != nil {
		return nil, err
	}

	return nil, p.driver.StartDriver(ctx, client, name, path)
}

// CheckPluginStatus 获取插件状态，包括enable:是否激活 on:是否成功运行
func (p *pluginImpl) CheckPluginStatus(ctx *comctx.Context) (any, error) {
	name := ctx.Param("name")
	plugin.PluginLock.Lock()
	client := plugin.PluginFactories[name]
	plugin.PluginLock.Unlock()

	if client == nil {
		return "", errors.New("plugin " + name + " no register")
	}

	enable, on := client.Status()
	var status map[string]int
	if enable && on {
		status = p.driver.GetDevicesStatusByDriver(ctx, name)
	}
	return PluginStatus{
		Name:   name,
		Enable: enable,
		On:     on,
		Status: status,
	}, nil
}

// CheckAllPluginStatus 获取所有已注册插件状态
func (p *pluginImpl) CheckAllPluginStatus(_ *comctx.Context) (any, error) {
	plugin.PluginLock.Lock()
	var plugStats []PluginStatus
	for name, client := range plugin.PluginFactories {
		var plug PluginStatus
		enable, on := client.Status()
		plug.Name = name
		plug.Enable = enable
		plug.On = on
		plugStats = append(plugStats, plug)
	}
	plugin.PluginLock.Unlock()
	return plugStats, nil
}

func (p *pluginImpl) GetAllConfigs(c *comctx.Context) (any, error) {
	plugin.PluginLock.Lock()
	var plugins []string
	for name := range plugin.PluginFactories {
		plugins = append(plugins, name)
	}
	result := p.driver.GetAllConfigs(c, plugins)
	plugin.PluginLock.Unlock()
	return result, nil
}

func (p *pluginImpl) UpdateNorthLink(c *comctx.Context) (any, error) {
	northConfig := &config.UplinkConfig{}
	if err := c.LoadBody(northConfig); err != nil {
		return nil, err
	}
	if len(northConfig.HTTPConfig) == 0 && len(northConfig.MQTTConfig) == 0 && len(northConfig.WebsocketConfig) == 0 {
		return nil, comctx.Error(comctx.ErrRequestParamInvalid)
	}
	err := p.driver.UpdateNorthLink(northConfig)
	return nil, err
}
