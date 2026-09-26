package handler

import (
	"github.com/gin-gonic/gin"

	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/service"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

type AirStoppageHandler struct{ service *service.AirStoppageService }

func NewAirStoppageHandler(service *service.AirStoppageService) *AirStoppageHandler {
	return &AirStoppageHandler{service: service}
}

func (h *AirStoppageHandler) List(c *gin.Context) {
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

func (h *AirStoppageHandler) Preview(c *gin.Context) {
	var input dto.PreviewAirStoppageRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		api.BindError(c, err)
		return
	}
	evaluation, err := h.service.Preview(c.Request.Context(), input.EdgeID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, evaluation)
}

func (h *AirStoppageHandler) Create(c *gin.Context) {
	actor, err := ActorFromContext(c)
	if err != nil {
		api.Fail(c, err)
		return
	}
	var input dto.CreateAirStoppageRequest
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

func (h *AirStoppageHandler) Restore(c *gin.Context) {
	id, ok := ParseID(c)
	if !ok {
		return
	}
	actor, err := ActorFromContext(c)
	if err != nil {
		api.Fail(c, err)
		return
	}
	item, err := h.service.Restore(c.Request.Context(), id, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, item)
}
