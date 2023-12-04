package north

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baetyl/baetyl-go/v2/comctx"
	dm "github.com/baetyl/baetyl-go/v2/dmcontext"
	"github.com/baetyl/baetyl-go/v2/mqtt"
	"github.com/baetyl/baetyl-go/v2/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/baetyl/baetyl-gateway/config"
)

func TestHttpNorthClient(t *testing.T) {
	confString := `
plugin:
 drivers:
server:
 port: ":9889"
mqttConfig:
 address: mqtt://127.0.0.1:8963
 cleansession: true
 username: test
 password: hahaha
northLink:
  httpConfig:
    - address: "http://127.0.0.1:9005"
      syncMaxGoroutine: 10
      path: "v1/list"
      method: "GET"
    - address: "http://127.0.0.1:9006"
      syncMaxGoroutine: 10
      path: "v1/list"
      method: "GET"
  mqttConfig:
    - address: mqtt://127.0.0.1:1883
      cleansession: true
convertFunc:
  - MsgBae64Encode
`
	dir, err := ioutil.TempDir("", "config")
	assert.NoError(t, err)
	fileName := "config_test"
	f, err := os.Create(filepath.Join(dir, fileName))
	defer f.Close()
	_, err = io.WriteString(f, confString)
	assert.NoError(t, err)
	conf := config.Config{}
	err = utils.LoadYAML(filepath.Join(dir, fileName), &conf)
	assert.NoError(t, err)
	go func() {
		r := gin.Default()
		a := 1
		r.GET("/v1/list", func(c *gin.Context) {
			fmt.Println("1号收到请求", a)
			a++
			c.JSON(200, gin.H{
				"message": "HelloWorld",
			})
			return
		})
		err := r.Run("127.0.0.1:9006")
		assert.NoError(t, err)
	}()

	go func() {
		r := gin.Default()
		a := 1
		r.GET("/v1/list", func(c *gin.Context) {
			fmt.Println("2号收到请求", a)
			a++
			c.JSON(200, gin.H{
				"message": "HelloWorld",
			})
			return
		})
		err := r.Run("127.0.0.1:9005")
		assert.NoError(t, err)
	}()
	north, err := CreateNorthClient(conf, "test")
	assert.NoError(t, err)
	time.Sleep(1 * time.Second)
	for i := 0; i < 100; i++ {
		err := north.Online(comctx.NewContextEmpty(), "node", &dm.DeviceInfo{
			Name:           "test",
			Version:        "1",
			DeviceModel:    "",
			AccessTemplate: "",
			DeviceTopic: dm.DeviceTopic{
				LifecycleReport: mqtt.QOSTopic{
					QOS:   1,
					Topic: "thing/custom-simulator/custom-zx/lifecycle/post",
				},
			},
			AccessConfig: nil,
		})
		assert.NoError(t, err)
	}
	time.Sleep(10 * time.Second)
}
