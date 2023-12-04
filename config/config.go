package config

import (
	"time"

	plugin "github.com/baetyl/baetyl-gateway-sdk/sdk/golang"
	"github.com/baetyl/baetyl-go/v2/http"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	"github.com/baetyl/baetyl-go/v2/utils"
	"github.com/baetyl/baetyl-go/v2/websocket"
)

// Config gateway main config
type Config struct {
	Logger      log.Config        `yaml:"logger" json:"logger"`
	Server      Server            `yaml:"server" json:"server"`
	Uplink      UplinkConfig      `yaml:"uplink" json:"uplink"`
	MqttConfig  mqtt.ClientConfig `yaml:"mqttConfig" json:"mqttConfig"`
	ConvertFunc []string          `yaml:"convertFunc" json:"convertFunc"`
	Plugin      struct {
		Drivers []plugin.DriverConfig `yaml:"drivers" json:"drivers" default:"[]"`
	} `yaml:"plugin" json:"plugin"`
	MessageCacheConfig  MessageCacheConfig `yaml:"messageCacheConfig" json:"messageCacheConfig"`
	SendMessageInterval time.Duration      `yaml:"sendMessageInterval" json:"sendMessageInterval" default:"3s"`
	MaxInterval         time.Duration      `yaml:"maxInterval" json:"maxInterval" default:"60s"`
	MinInterval         time.Duration      `yaml:"minInterval" json:"minInterval" default:"2s"`
}

type UplinkConfig struct {
	HTTPConfig      []NorthHTTPConfig `yaml:"httpConfig" json:"httpConfig"`
	MQTTConfig      []NorthMQTTConfig `yaml:"mqttConfig" json:"mqttConfig"`
	WebsocketConfig []NorthWsConfig   `yaml:"wsConfig" json:"wsConfig"`
}

type NorthHTTPConfig struct {
	http.ClientConfig `yaml:",inline" json:",inline"`
	Path              string            `yaml:"path" json:"path"`
	Method            string            `yaml:"method" json:"method"`
	Headers           map[string]string `yaml:"headers" json:"headers"`
	RetryTimes        int               `yaml:"retryTimes" json:"retryTimes" default:"1"`
	ID                string            `yaml:"id" json:"id"`
}

type NorthMQTTConfig struct {
	ID                string `yaml:"id" json:"id"`
	mqtt.ClientConfig `yaml:",inline" json:",inline"`
	RetryTimes        int `yaml:"retryTimes" json:"retryTimes" default:"1"`
}

type NorthWsConfig struct {
	ID                     string `yaml:"id" json:"id"`
	websocket.ClientConfig `yaml:",inline" json:",inline"`
	RetryTimes             int `yaml:"retryTimes" json:"retryTimes" default:"1"`
}

//type DriverConfig struct {
//	Name       string `yaml:"name" json:"name"`
//	BinPath    string `yaml:"binPath" json:"binPath"`
//	ConfigPath string `yaml:"configPath" json:"configPath"`
//}

// Server server config
type Server struct {
	Port         string            `yaml:"port" json:"port"`
	ReadTimeout  time.Duration     `yaml:"readTimeout" json:"readTimeout" default:"30s"`
	WriteTimeout time.Duration     `yaml:"writeTimeout" json:"writeTimeout" default:"30s"`
	ShutdownTime time.Duration     `yaml:"shutdownTime" json:"shutdownTime" default:"3s"`
	Certificate  utils.Certificate `yaml:",inline" json:",inline"`
}

type Entry struct {
	Entry string `yaml:"entry" json:"entry" validate:"nonzero"`
}

type DeviceProperty struct {
	PropName string      `yaml:"propName" json:"propName"`
	PropVal  interface{} `yaml:"propVal" json:"propVal"`
}

type MessageCacheConfig struct {
	FilePath      string        `yaml:"filePath" json:"filePath" default:"/var/lib/baetyl/store"`
	Enable        bool          `yaml:"enable" json:"enable"`
	CacheDuration time.Duration `yaml:"cacheDuration" json:"cacheDuration" default:"24h"`
	// interval between loop executions
	Interval time.Duration `yaml:"interval" json:"interval" default:"3s"`
	// support disk memory
	CacheType string `yaml:"cacheType" json:"cacheType" default:"disk"`
	//clear history cache data
	Clear        bool   `yaml:"clear" json:"clear"`
	MaxMemoryUse string `yaml:"maxMemoryUse" json:"maxMemoryUse" default:"100MB"`
	FailChanSize int    `yaml:"failChanSize" json:"failChanSize" default:"100"`
}
