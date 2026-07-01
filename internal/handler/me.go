package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/auth"
)

type meHandler struct{}

func NewMeHandler() *meHandler { return &meHandler{} }

// RegisterRoutes монтирует защищённые «identity»-роуты в группу (обычно /api/v1,
// уже под JWT-мидлварью).
func (h *meHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/me", h.me)
}

// me godoc
// @Summary  Текущий аутентифицированный пользователь (из JWT)
// @Tags     auth
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  401 {object} object
// @Router   /api/v1/me [get]
func (h *meHandler) me(c *gin.Context) {
	cl := auth.ClaimsFromContext(c.Request.Context())
	if cl == nil {
		// Сюда попадём, только если keycloak.enabled=false (мидлварь не навешана);
		// при включённом Keycloak невалидный токен отсекается раньше, в мидлвари.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"subject":  cl.Subject,
		"username": cl.Username,
		"email":    cl.Email,
		"roles":    cl.Roles,
	})
}
