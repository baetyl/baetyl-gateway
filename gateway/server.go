package gateway

import (
	"context"
	"io"
	"net/http"

	"github.com/baetyl/baetyl-go/v2/comctx"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/utils"
	"github.com/gin-gonic/gin"

	"github.com/baetyl/baetyl-gateway/api"
	"github.com/baetyl/baetyl-gateway/config"
	"github.com/baetyl/baetyl-gateway/models"
)

type sucResponse struct {
	Success bool `json:"success"`
}

type Server interface {
	Run()
	InitRouter()
	io.Closer
}

type serverImpl struct {
	cfg    config.Config
	router *gin.Engine
	driver models.DriverEngine
	plug   api.PluginAPI
	dev    api.DeviceAPI
	server *http.Server
	log    *log.Logger
}

func NewGatewayServer(cfg config.Config, driver models.DriverEngine) (Server, error) {
	router := gin.New()
	plug := api.NewPluginAPI(driver)
	dev := api.NewDeviceAPI(driver)

	svr := &http.Server{
		Addr:           cfg.Server.Port,
		Handler:        router,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		MaxHeaderBytes: 1 << 20,
	}
	if cfg.Server.Certificate.Cert != "" &&
		cfg.Server.Certificate.Key != "" &&
		cfg.Server.Certificate.CA != "" {
		t, err := utils.NewTLSConfigServer(utils.Certificate{
			CA:             cfg.Server.Certificate.CA,
			Cert:           cfg.Server.Certificate.Cert,
			Key:            cfg.Server.Certificate.Key,
			ClientAuthType: cfg.Server.Certificate.ClientAuthType,
		})
		if err != nil {
			return nil, err
		}
		svr.TLSConfig = t
	}

	return &serverImpl{
		cfg:    cfg,
		router: router,
		driver: driver,
		plug:   plug,
		dev:    dev,
		server: svr,
		log:    log.L().With(log.Any("baetyl-gateway", "server")),
	}, nil
}

func (s *serverImpl) InitRouter() {
	s.router.GET("/health", Health)
	v1 := s.router.Group("v1")
	{
		plugin := v1.Group("/plugins")
		{
			plugin.POST("/:name", comctx.Wrapper(s.plug.CreateAndActivatePlugin))
			plugin.PUT("/:name/activate", comctx.Wrapper(s.plug.ActivatePlugin))
			plugin.PUT("/:name/deactivate", comctx.Wrapper(s.plug.ClosePlugin))
			plugin.PUT("/:name/restart", comctx.Wrapper(s.plug.RestartPlugin))
			plugin.GET("/:name/status", comctx.Wrapper(s.plug.CheckPluginStatus))
			plugin.GET("/status", comctx.Wrapper(s.plug.CheckAllPluginStatus))
			plugin.GET("/all", comctx.Wrapper(s.plug.GetAllConfigs))
			plugin.PUT("/:name", comctx.Wrapper(nil))
			plugin.DELETE("/:name", comctx.Wrapper(nil))
			plugin.PUT("/northlink", comctx.Wrapper(s.plug.UpdateNorthLink))
		}

		devices := v1.Group("/devices")
		{
			devices.GET("/:name/shadow", comctx.Wrapper(s.dev.GetDeviceShadow))
			devices.GET("/:name/property/:id", comctx.Wrapper(s.dev.GetLatestDeviceProperty))
			devices.POST("/:name/shadow", comctx.Wrapper(s.dev.SetDeviceNumber))
			devices.POST("/:name/event", comctx.Wrapper(s.dev.PublishEvent))
		}
	}
}

func (s *serverImpl) Run() {
	if s.server.TLSConfig == nil {
		s.log.Info("gateway server http start", log.Any("server", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil {
			s.log.Info("gateway server http stopped", log.Error(err))
		}
	} else {
		s.log.Info("gateway server https start", log.Any("server", s.server.Addr))
		if err := s.server.ListenAndServeTLS("", ""); err != nil {
			s.log.Info("gateway server https stopped", log.Error(err))
		}
	}
}

func (s *serverImpl) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTime)
	defer cancel()
	return s.server.Shutdown(ctx)
}

type HandlerFunc func(c *gin.Context) (any, error)

func Health(c *gin.Context) {
	c.JSON(PackageResponse(nil))
}

// PackageResponse PackageResponse
func PackageResponse(res interface{}) (int, interface{}) {
	if res == nil {
		res = &sucResponse{
			Success: true,
		}
	}
	return http.StatusOK, res
}
