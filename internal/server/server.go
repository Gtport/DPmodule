package server

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/adapter/asu"
	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/adapter/reference"
	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/handler"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/secret"
	"github.com/Gtport/DPmodule/internal/service"
	"github.com/Gtport/DPmodule/pkg/metrics"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

// Build constructs the http.Server with all routes and middleware wired up.
// mountMetrics controls whether /metrics is served on this (main) server;
// when false, metrics are served on a dedicated server (see NewMetricsServer).
func Build(
	cfg *config.Config,
	db *sql.DB,
	cfgCache *service.ConfigCache,
	dirCache *service.DirectoryCache,
	dislRepo port.DislocationRepository,
	actualCache *service.ActualCache,
	status9Cache *service.Status9Cache,
	status6Cache *service.Status6Cache,
	historyRepo port.HistoryRepository,
	unplannedRepo port.UnplannedMoveRepository,
	planRepo port.PlanRepository,
	journalRepo port.JournalRepository,
	adminRepo port.AdminTablesRepository,
	vagonOpRepo port.VagonOperationRepository,
	jwtMW *middleware.KeycloakJWT,
	log *zap.Logger,
	mountMetrics bool,
) (*http.Server, *service.ASUIngest, *service.ReferenceService, *service.VagonOpService) {
	// asuIngest и refSvc отдаём наружу: их фоновые крон-воркеры живут в main
	// (жизненный цикл процесса), а ручки остаются здесь. asuIngest = nil, если нет
	// БД/справочников (тогда воркер не запускается).
	var asuIngest *service.ASUIngest
	var vagonOps *service.VagonOpService

	if cfg.App.Env != "dev" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// ---- global middleware ----
	router.Use(
		middleware.InjectLogger(log),
		middleware.Recover(log),
		middleware.RequestID(),
		middleware.RequestLogger(),
		metrics.Middleware(),
	)

	// ---- system routes (no auth) ----
	handler.NewHealthHandler(db).RegisterRoutes(router)
	if mountMetrics {
		router.GET("/metrics", metrics.Handler())
	}
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ---- protected API routes ----
	// jwtMW may be nil when keycloak is disabled — guard so the template still
	// boots and serves system routes. Реальные маршруты (dislocation и т.д.)
	// монтируются здесь, в группе /api/v1.
	api := router.Group("/api/v1")
	if jwtMW != nil {
		api.Use(jwtMW.Middleware())
	}
	handler.NewMeHandler().RegisterRoutes(api)

	// Админ-редактор справочников (реестр list_tables) — только administrator.
	// Правки применяются к снимку отдельной кнопкой «Обновить справочники».
	if adminRepo != nil {
		adminGrp := api.Group("")
		if jwtMW != nil {
			adminGrp.Use(jwtMW.RequireRole(auth.RoleAdministrator))
		}
		handler.NewAdminTablesHandler(service.NewAdminTables(adminRepo)).RegisterRoutes(adminGrp)
	}

	// Экран «Прогнозы»: сводка поездов с прогнозными полями Stage 3/4 из RAM-снимка.
	if actualCache != nil {
		handler.NewForecastHandler(service.NewForecastBoard(actualCache)).RegisterRoutes(api)
	}

	// Экран «Пропавшие вагоны»: записи-8 из таблицы кандидатов (status9).
	if status9Cache != nil {
		handler.NewMissingHandler(service.NewMissingService(status9Cache, status6Cache)).RegisterRoutes(api)
	}

	// Памятки на подачу/уборку (внешний провайдер, тот же что дислокация). Не зависит
	// от БД — на этом этапе данные только логируются, не сохраняются. Ручной забор по
	// номеру и ручной триггер инкремента — здесь; крон-инкремент запускает main.
	refClient := reference.NewHTTPClient(cfg.Reference.BaseURL, cfg.Reference.InsecureTLS, cfg.Reference.AuthSecretKey, secret.NewEnvSource())
	refSvc := service.NewReferenceService(refClient, cfg.Reference.Clients, cfg.Reference.PullInterval, log)
	handler.NewReferenceHandler(refSvc).RegisterRoutes(api)

	// Приём файлов ЛК (шаг 1) — только если справочники и настроечная таблица
	// загружены (требует БД). Формат — из ConfigCache, «чей файл» (ОКПО→терминалы)
	// — из DirectoryCache (ports).
	if cfgCache != nil && dirCache != nil {
		lkIntake := service.NewLKIntake(cfgCache, dirCache, cfg.Storage.BaseDir)
		handler.NewLKUploadHandler(lkIntake).RegisterRoutes(api)

		// Шаг 2 (обработка в снимок) — требует репозиторий дислокации (БД).
		if dislRepo != nil {
			// Единый журнал событий (обновления дислокации, загрузки планов).
			journal := service.NewJournal(journalRepo, log)

			proc := service.NewLKProcessor(lkIntake, dislRepo, actualCache, status9Cache, status6Cache, historyRepo)
			proc.SetJournal(journal)
			// «Бесплановые в подходе» (Оперативка): трекинг на сравнении снимков.
			if unplannedRepo != nil {
				proc.SetUnplannedRepo(unplannedRepo)
			}
			handler.NewLKProcessHandler(proc).RegisterRoutes(api)

			// «Обновить справочники»: горячая перезагрузка словарей + гибридный
			// пересчёт снимка (правки cargo/marka доезжают до вагонов) + Stage 3–4.
			handler.NewDictReloadHandler(proc).RegisterRoutes(api)

			// Приём плана подвода: разбор + матч + простановка PlanMsk в снимок.
			// Целевые площадки — из DirectoryCache (ports.plan_code).
			planProc := service.NewPlanProcessor(dirCache, dislRepo, actualCache, planRepo, cfg.Storage.BaseDir)
			planProc.SetJournal(journal)
			planProc.SetConfig(cfgCache) // порог свежести дислокации для гарда загрузки плана
			handler.NewPlanUploadHandler(planProc).RegisterRoutes(api)

			// Автозагрузка дислокации из АСУ-АСУ (ingest=api_pull): забор всех клиентов,
			// сверка меток формирования и пересборка снимка тем же конвейером (proc). По
			// расписанию — внутренний крон-воркер (см. main.go); ручной триггер — ручка
			// POST /dislocation/asu/pull. Транспорт — HTTP-адаптер, ключ к АСУ из env.
			secrets := secret.NewEnvSource()
			asuFactory := func(dc domain.DataSourceConfig) port.ASUClient { return asu.NewHTTPClient(dc, secrets) }
			asuIngest = service.NewASUIngest(cfgCache, asuFactory, proc, log)
			asuIngest.SetJournal(journal)
			handler.NewASUPullHandler(asuIngest).RegisterRoutes(api)

			// История продвижения вагона (запрос 601, тот же провайдер): очередь
			// заявок из конвейера (прибытие/пропажа/выбытие-10) + ручной запрос.
			// Клиент собирается из того же источника data_source id=asu.
			if vagonOpRepo != nil {
				if ds, ok := cfgCache.DataSource("asu"); ok && ds.Enabled {
					histClient := asu.NewHTTPClient(ds.Config, secrets)
					vagonOps = service.NewVagonOpService(vagonOpRepo, histClient, dirCache, actualCache, log)
					vagonOps.SetHistory(historyRepo) // «История движения вагона»: рейс из vagon_history
					vagonOps.SetLimits(cfg.WagonOps.Batch, cfg.WagonOps.Pause, cfg.WagonOps.MaxAttempts)
					proc.SetVagonOps(vagonOps)
					handler.NewVagonOpsHandler(vagonOps).RegisterRoutes(api)
				}
			}

			// Статус-панель: актуальность дислокации и планов из журнала.
			handler.NewStatusHandler(service.NewStatusService(journal, dirCache)).RegisterRoutes(api)

			// Экран «Перестановки/Переадресация»: группировки из RAM-снимка,
			// батч-правка naznach/pereadr_* с одним пересчётом Stage 3–4.
			handler.NewRearrangeHandler(service.NewRearrangeService(proc)).RegisterRoutes(api)

			// «История прибывших» домашней страницы: чтение vagon_history (веха
			// прибытия из Stage 2), правки истории и подтверждение/отклонение
			// кандидатов прибытия (статус 9) через конвейер proc.
			handler.NewArrivalsHandler(service.NewArrivalsService(historyRepo, dirCache, proc)).RegisterRoutes(api)

			// «Ближайшие поезда» домашней страницы: подходящие поезда из снимка
			// (план → прогноз → расчёт), только чтение.
			handler.NewNearestHandler(service.NewNearestService(actualCache, dirCache)).RegisterRoutes(api)

			// «Оперативка» домашней страницы: суточные счётчики по терминалам
			// (вехи истории + статус 10 из снимка), только чтение.
			opSvc := service.NewOperativkaService(historyRepo, actualCache, dirCache, unplannedRepo)
			opSvc.SetJournal(journal)
			handler.NewOperativkaHandler(opSvc).RegisterRoutes(api)
		}
	}

	return &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}, asuIngest, refSvc, vagonOps
}

// NewMetricsServer returns a minimal http.Server that serves /metrics only,
// on its own port — kept off the public API surface.
func NewMetricsServer(host string, port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.StdHandler())
	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}
}
