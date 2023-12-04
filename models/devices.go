package models

import (
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
)

type DeviceMetaShadow struct {
	Attributes map[string]interface{} `json:"attrs,omitempty"`
}

type DeviceInfo struct {
	dm.DeviceInfo `json:",inline"`
	Driver        string `json:"driver"`
}

type AccessTemplate struct {
	dm.AccessTemplate `json:",inline"`
	DeviceModel       string `json:"deviceModel,omitempty"`
}

type AllPluginConfigs struct {
	DeviceModels   map[string][]dm.DeviceProperty
	AccessTemplate map[string]AccessTemplate
	Devices        map[string]DeviceInfo
}

type FullDriverConfig struct {
	Devices []DeviceInfo `yaml:"devices" json:"devices"`
	Driver  string       `yaml:"driver" json:"driver"`
}

type DeviceModelPropertyYaml struct {
	Name           string                   `json:"name,omitempty" yaml:"name,omitempty"`
	Type           string                   `json:"type,omitempty" yaml:"type,omitempty"`
	Mode           string                   `json:"mode,omitempty" yaml:"mode,omitempty"`
	Format         string                   `json:"format,omitempty" yaml:"format,omitempty"`                        // 当 Type 为 date/time 时使用
	EnumType       dm.EnumType              `json:"enumType,omitempty" yaml:"enumType,omitempty" binding:"dive"`     // 当 Type 为 enum 时使用
	ArrayType      dm.ArrayType             `json:"arrayType,omitempty" yaml:"arrayType,omitempty" binding:"dive"`   // 当 Type 为 array 时使用
	ObjectType     map[string]dm.ObjectType `json:"objectType,omitempty" yaml:"objectType,omitempty" binding:"dive"` // 当 Type 为 object 时使用
	ObjectRequired []string                 `json:"objectRequired,omitempty" yaml:"objectRequired,omitempty"`        // 当 Type 为 object 时, 记录必填字段
}

type SubDevices struct {
	Devices []dm.DeviceInfo `json:"devices" yaml:"devices"`
	Driver  string          `json:"driver" yaml:"driver"`
}

type PluginConfig struct {
	Models         map[string][]DeviceModelPropertyYaml `json:"models"`
	AccessTemplate map[string]dm.AccessTemplate         `json:"access_template"`
	SubDevices     SubDevices                           `json:"sub_devices"`
}
