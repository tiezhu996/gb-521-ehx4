package handler

import (
	"github.com/gin-gonic/gin"

	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/service"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

type VentilationStoppageHandler struct {
	service *service.VentilationStoppageService
}

func NewVentilationStoppageHandler(service *service.VentilationStoppageService) *VentilationStoppageHandler {
	return &VentilationStoppageHandler{service: service}
}

func (h *VentilationStoppageHandler) List(c *gin.Context) {
	var query dto.StoppageListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		api.BindError(c, err)
		return
	}
	items, total, page, pageSize, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, items, page, pageSize, total)
}

func (h *VentilationStoppageHandler) Get(c *gin.Context) {
	id, ok := ParseID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, item)
}

func (h *VentilationStoppageHandler) Preview(c *gin.Context) {
	var input dto.PreviewStoppageRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		api.BindError(c, err)
		return
	}
	assessment, err := h.service.Preview(c.Request.Context(), input)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, assessment)
}

func (h *VentilationStoppageHandler) Create(c *gin.Context) {
	actor, err := ActorFromContext(c)
	if err != nil {
		api.Fail(c, err)
		return
	}
	var input dto.CreateStoppageRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		api.BindError(c, err)
		return
	}
	item, err := h.service.Create(c.Request.Context(), input, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Created(c, item)
}

func (h *VentilationStoppageHandler) Recover(c *gin.Context) {
	id, ok := ParseID(c)
	if !ok {
		return
	}
	actor, err := ActorFromContext(c)
	if err != nil {
		api.Fail(c, err)
		return
	}
	// 恢复说明可选；没有请求体时按空说明处理。
	var input dto.RecoverStoppageRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			api.BindError(c, err)
			return
		}
	}
	item, err := h.service.Recover(c.Request.Context(), id, input.Note, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, item)
}
