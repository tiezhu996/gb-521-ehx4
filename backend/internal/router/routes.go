package router

import (
	"github.com/gin-gonic/gin"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/handler"
	"mine-ventilation-network-simulator/backend/internal/middleware"
)

func registerVentilationNodeRoutes(group *gin.RouterGroup, h *handler.VentilationNodeHandler) {
	group.GET("/network/validate", h.ValidateNetwork)
	nodes := group.Group("/nodes")
	nodes.GET("", h.List)
	nodes.GET("/:id", h.Get)
	nodes.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Create)
	nodes.PUT("/:id", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Update)
}

func registerAirwayEdgeRoutes(group *gin.RouterGroup, h *handler.AirwayEdgeHandler) {
	edges := group.Group("/edges")
	edges.GET("", h.List)
	edges.GET("/:id", h.Get)
	edges.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Create)
	edges.PUT("/:id", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Update)
}

func registerFanScenarioRoutes(group *gin.RouterGroup, h *handler.FanScenarioHandler) {
	scenarios := group.Group("/scenarios")
	scenarios.GET("", h.List)
	scenarios.GET("/:id", h.Get)
	scenarios.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Create)
	scenarios.POST("/:id/transition", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleReviewer), string(constants.RoleAdmin)), h.Transition)
}

func registerSimulationRoutes(group *gin.RouterGroup, h *handler.SimulationRunHandler, limiter *middleware.RateLimiter) {
	runs := group.Group("/simulations")
	runs.GET("", h.List)
	runs.GET("/:id", h.Get)
	runs.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), limiter.Middleware(), h.Start)
	runs.POST("/:id/confirm-risks", middleware.RBACMiddleware(string(constants.RoleReviewer), string(constants.RoleAdmin)), h.ConfirmRisks)
}

func registerAirStoppageRoutes(group *gin.RouterGroup, h *handler.AirStoppageHandler) {
	stoppages := group.Group("/stoppages")
	stoppages.GET("", h.List)
	stoppages.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Create)
	stoppages.POST("/preview", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Preview)
	stoppages.POST("/:id/restore", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Restore)
}
