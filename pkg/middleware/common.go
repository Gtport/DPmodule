package middleware

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/pkg/logger"
)

const (
	requestIDHeader = "X-Request-Id"
	// requestIDKey — полный request_id в контексте gin: строка лога несёт
	// укороченный, а хендлерам (и ответу об ошибке) нужен целый.
	requestIDKey = "request_id"
)

// errBodyLimit — сколько байт тела неуспешного ответа забираем в лог. Тексты
// наших ошибок — одна русская фраза, 2 КБ хватает с запасом; ограничение стоит
// против чужих многословных ответов (панику отдаёт сам gin, ошибку разбора
// JSON — биндер) и на случай, если ручка вернёт ошибкой большой документ.
const errBodyLimit = 2 << 10

// errorCapture — обёртка над gin.ResponseWriter, снимающая копию тела ТОЛЬКО у
// неуспешных ответов (статус ≥ 400).
//
// Зачем: причина ошибки живёт в теле. Хендлеры (175 мест) отвечают
// `c.JSON(status, gin.H{"error": err.Error()})` и в лог не пишут ничего, так
// что в файле оставалась строка `http status=500` без единого слова о том, что
// сломалось. Перехват в одном месте закрывает все ручки разом — и будущие тоже,
// без обязанности помнить про лог в каждой новой.
//
// Успешные ответы не копируются никогда: среди них выгрузки Excel (история —
// 100 тыс. строк, 11 МБ) и тайлы карты, держать их в памяти вторым экземпляром
// незачем.
type errorCapture struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *errorCapture) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

// WriteString переопределён вместе с Write: gin отдаёт им разные ответы
// (c.JSON идёт через Write, c.String — через WriteString), и без этого метода
// часть ошибок терялась бы, потому что встроенный писатель мимо нас.
func (w *errorCapture) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *errorCapture) capture(b []byte) {
	// Статус к моменту записи уже проставлен: c.JSON/c.String сначала зовут
	// c.Status(code), и только потом рендерят тело.
	if w.Status() < http.StatusBadRequest {
		return
	}
	if free := errBodyLimit - w.body.Len(); free > 0 {
		w.body.Write(b[:min(len(b), free)])
	}
}

// InjectLogger puts the root zap logger into every request's context.
func InjectLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), log))
		c.Next()
	}
}

// Recover catches panics, logs them, and returns 500.
func Recover(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if p := recover(); p != nil {
				// Логгер берём из контекста: там уже лежит request_id, и паника
				// связывается со строкой http того же запроса. Корневой log —
				// запасной путь, если Recover окажется выше RequestID в цепочке.
				logger.FromContextOr(c.Request.Context(), log).Error("паника в обработчике",
					logger.Comp(logger.CompHTTP),
					zap.Any("panic", p),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.Stack("stack"),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

// RequestID injects a request-id into the context and response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Header(requestIDHeader, rid)
		c.Set(requestIDKey, rid)

		// В строку лога идёт КОРОТКИЙ идентификатор: полный UUID — 36 знаков на
		// каждой записи, а связать строки внутри файла хватает восьми. Полное
		// значение остаётся в заголовке ответа (по нему диспетчер и приходит).
		log := logger.FromContext(c.Request.Context()).With(zap.String("req", shortID(rid)))
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), log))
		c.Next()
	}
}

// RequestLogger logs method, path, status, and latency for every request.
// Уровень записи выбирается по статусу (см. levelFor), у неуспешных ответов
// добавляется причина из тела и имя пользователя из токена.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		cap := &errorCapture{ResponseWriter: c.Writer}
		c.Writer = cap

		// Пишем в defer: паника разворачивает стек мимо обычного пути, и без
		// defer запрос, уронивший обработчик, не оставил бы строки http вовсе —
		// только запись Recover, без латентности и пути.
		defer func() {
			status := c.Writer.Status()

			// Личность вызывающего — в колонке цели: «← иванов 10.0.0.14».
			// Раньше имя добавлялось полем и только у неуспешных, а «кто ходит»
			// — первый вопрос при разборе жалобы, в том числе по успешным.
			cl := auth.ClaimsFromContext(c.Request.Context())
			fields := logger.In(logger.CompHTTP, caller(cl)+" "+c.ClientIP())
			fields = append(fields,
				zap.String("method", c.Request.Method),
				zap.String("path", c.Request.URL.Path),
				zap.Int("status", status),
				zap.Duration("took", time.Since(start)),
			)
			// Query кладём только у неуспешных: у отчётов и выгрузок там весь
			// фильтр (период, терминалы, список вагонов), и в успешной строке
			// это лишний шум, а в разборе отказа — половина ответа на «почему».
			if raw := c.Request.URL.RawQuery; raw != "" && status >= http.StatusBadRequest {
				fields = append(fields, zap.String("query", raw))
			}
			if cap.body.Len() > 0 {
				// ToValidUTF8: лимит режет по байтам, а тексты ошибок русские —
				// обрезка попадает в середину буквы, и в JSON-логе оставался бы
				// символ замены. Отсекаем недобитую руну вместо неё.
				fields = append(fields, zap.String("error",
					strings.ToValidUTF8(strings.TrimSpace(cap.body.String()), "")))
			}

			// Сообщение — утверждение о результате, а не имя подсистемы: имя
			// теперь стоит в своей колонке, а «http» в тексте ничего не сообщало.
			msg := "запрос обработан"
			if status >= http.StatusBadRequest {
				msg = "запрос отклонён"
			}
			log := logger.FromContext(c.Request.Context())
			if ce := log.Check(levelFor(status, c.Request.URL.Path), msg); ce != nil {
				ce.Write(fields...)
			}
		}()

		c.Next()
	}
}

// caller — имя вызывающего для колонки цели. Аноним — это либо публичная ручка
// (тайлы карты, health), либо запрос без токена: и то, и другое надо отличать
// от входа известного пользователя.
func caller(cl *auth.Claims) string {
	if cl == nil || cl.Username == "" {
		return "аноним"
	}
	return cl.Username
}

// shortID — первые 8 символов request_id. UUID генерируется случайным, и
// восьми знаков достаточно, чтобы связать строки одного запроса в файле;
// заголовок ответа при этом несёт значение целиком.
func shortID(rid string) string {
	if len(rid) > 8 {
		return rid[:8]
	}
	return rid
}

// levelFor — уровень строки http. Отказ виден по уровню, а не вычитыванием
// поля status: 5xx — наша поломка (Error), 4xx — отказ клиенту, чаще всего
// осознанный гард или права (Warn).
//
// Успешные обращения к пробам и тайлам уходят на Debug: docker-healthcheck
// дёргает /ready каждые несколько секунд, а один экран карты — это десятки
// тайлов, и на Info они забивают файл, в котором ищут работу диспетчера.
// Их ОТКАЗЫ при этом остаются на Warn/Error — тишины по ошибке не наступает.
func levelFor(status int, path string) zapcore.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return zapcore.ErrorLevel
	case status >= http.StatusBadRequest:
		return zapcore.WarnLevel
	case isQuietPath(path):
		return zapcore.DebugLevel
	default:
		return zapcore.InfoLevel
	}
}

func isQuietPath(path string) bool {
	switch path {
	case "/health", "/ready", "/metrics":
		return true
	}
	return strings.HasPrefix(path, "/api/v1/map/tiles/")
}
