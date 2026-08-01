package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/adapter/lkrobot"
	"github.com/Gtport/DPmodule/internal/service"
)

// lkRobotHandler — автовыгрузка дислокации из личного кабинета РЖД: то же, что
// диспетчер делал руками (зайти в ЛК, заказать отчёт, скачать xlsx, загрузить).
//
// Пароль приходит в теле запроса и живёт только на время запуска: в БД, конфиге
// и логах его нет. Логины — в настроечной таблице lk_account (админ-редактор).
// Расписания пока нет: запуск кнопкой диспетчера.
type lkRobotHandler struct {
	robot *service.LKRobot
}

func NewLKRobotHandler(robot *service.LKRobot) *lkRobotHandler {
	return &lkRobotHandler{robot: robot}
}

func (h *lkRobotHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/lk/robot/accounts", h.accounts)
	g.POST("/dislocation/lk/robot/run", h.run)
}

// accounts godoc
// @Summary  Аккаунты ЛК РЖД для автовыгрузки (по одному на поток/порт)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  503 {object} object "аккаунты не заведены"
// @Router   /api/v1/dislocation/lk/robot/accounts [get]
func (h *lkRobotHandler) accounts(c *gin.Context) {
	accs, err := h.robot.Accounts(c.Request.Context())
	if err != nil {
		c.JSON(statusForLKRobotError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"accounts": accs})
}

// lkRobotRunRequest — пароли по потокам: диспетчер вводит их разом за оба порта.
type lkRobotRunRequest struct {
	Accounts []struct {
		OKPO     int64  `json:"okpo"`
		Password string `json:"password"`
	} `json:"accounts"`
}

// run godoc
// @Summary  Запуск автовыгрузки из ЛК РЖД (пароли вводит диспетчер, не хранятся)
// @Tags     dislocation
// @Security BearerAuth
// @Param    body body lkRobotRunRequest true "пароли по ОКПО"
// @Success  200 {object} object
// @Failure  400 {object} object "не передан ни один пароль"
// @Failure  503 {object} object "аккаунты не заведены"
// @Router   /api/v1/dislocation/lk/robot/run [post]
func (h *lkRobotHandler) run(c *gin.Context) {
	var req lkRobotRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не разобрать тело запроса"})
		return
	}
	passwords := make(map[int64]string, len(req.Accounts))
	for _, a := range req.Accounts {
		if a.Password != "" {
			passwords[a.OKPO] = a.Password
		}
	}
	if len(passwords) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не введён ни один пароль"})
		return
	}

	res, err := h.robot.Run(c.Request.Context(), passwords)
	if err != nil {
		c.JSON(statusForLKRobotError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// statusForLKRobotError — доменные ошибки робота в HTTP-статусы. Отказы по
// отдельным потокам сюда не попадают: они едут в теле ответа (один порт может
// не пустить, второй при этом обновится).
func statusForLKRobotError(err error) int {
	switch {
	case errors.Is(err, service.ErrNoLKAccounts):
		return http.StatusServiceUnavailable
	case errors.Is(err, lkrobot.ErrAuth):
		return http.StatusUnauthorized
	default:
		return http.StatusBadGateway
	}
}
