package handler

import (
	"net/http"
	"strings"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/middleware"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/service"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/util"
	"github.com/gin-gonic/gin"
)

type HeatHandler struct{ service service.HeatService }

func NewHeatHandler(s service.HeatService) *HeatHandler {
	return &HeatHandler{service: s}
}

func (h *HeatHandler) Register(group *gin.RouterGroup) {
	resource := group.Group("/heats")
	resource.GET("", h.list)
	resource.GET("/remelt-furnaces", h.remeltFurnaces)
	resource.GET("/:id", h.get)
	resource.POST("", middleware.RequireRoles(model.RoleOperator, model.RoleReviewer, model.RoleAdmin), h.create)
	resource.PUT("/:id", middleware.RequireRoles(model.RoleOperator, model.RoleReviewer, model.RoleAdmin), h.update)
	resource.POST("/:id/transition", middleware.RequireRoles(model.RoleOperator, model.RoleReviewer, model.RoleAdmin), h.transition)
	resource.DELETE("/:id", middleware.RequireRoles(model.RoleAdmin), h.remove)
}

func (h *HeatHandler) list(c *gin.Context) {
	query := bindPage(c)
	result, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		handleError(c, err)
		return
	}
	util.Page(c, result.Items, result.Page, result.PageSize, result.Total)
}

func (h *HeatHandler) remeltFurnaces(c *gin.Context) {
	heatCode := strings.TrimSpace(c.Query("heatCode"))
	if heatCode == "" {
		util.Fail(c, http.StatusBadRequest, "invalid_request", "heatCode query parameter is required")
		return
	}
	options, err := h.service.RemeltFurnaces(c.Request.Context(), heatCode)
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, options)
}

func (h *HeatHandler) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *HeatHandler) create(c *gin.Context) {
	var input dto.CreateHeat
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Create(c.Request.Context(), input, actorFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.Created(c, item)
}

func (h *HeatHandler) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.UpdateHeat
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Update(c.Request.Context(), id, input, actorFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *HeatHandler) transition(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.TransitionRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Transition(c.Request.Context(), id, input, actorFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *HeatHandler) remove(c *gin.Context) {
	if roleFromContext(c) != "admin" {
		util.Fail(c, http.StatusForbidden, "forbidden", "admin role is required")
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, actorFromContext(c), requestIDFromContext(c)); err != nil {
		handleError(c, err)
		return
	}
	util.NoContent(c)
}
