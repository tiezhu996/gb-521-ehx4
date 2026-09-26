package router

import (
	"github.com/gin-gonic/gin"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/handler"
	"mine-ventilation-network-simulator/backend/internal/middleware"
)

func registerVentilationStoppageRoutes(group *gin.RouterGroup, h *handler.VentilationStoppageHandler) {
	stoppages := group.Group("/stoppages")
	stoppages.GET("", h.List)
	stoppages.GET("/:id", h.Get)
	stoppages.POST("/preview", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Preview)
	stoppages.POST("", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleAdmin)), h.Create)
	stoppages.POST("/:id/recover", middleware.RBACMiddleware(string(constants.RoleEngineer), string(constants.RoleReviewer), string(constants.RoleAdmin)), h.Recover)
}
