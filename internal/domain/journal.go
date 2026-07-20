package domain

import "encoding/json"

// Типы событий единого журнала (event_journal.event_type).
const (
	EventDislUpdate   = "disl_update"   // снимок дислокации пересобран (ЛК/JSON)
	EventDislRejected = "disl_rejected" // обновление дислокации ОТКЛОНЕНО гардом (снимок не тронут)
	EventPlanUpload   = "plan_upload"   // загружен план подвода (МА/НК)
	EventDictReload   = "dict_reload"   // «Обновить справочники»: перезагрузка словарей + пересчёт снимка
	EventRearrange    = "rearrangement" // перестановка/переадресация: правка naznach/pereadr_* оператором
	EventArrivalsEdit = "arrivals_edit" // операторские действия с прибывшими: правки истории, подтверждения-скрытия
)

// Триггеры обновления снимка дислокации (event_journal.trigger).
const (
	TriggerManual        = "manual"        // ручное обновление (пользователь запустил обработку ЛК)
	TriggerScheduled     = "scheduled"     // по расписанию (фоновый воркер / приём АСУ) [задел]
	TriggerActualization = "actualization" // пересчёт текущего снимка без новых данных [задел]
	TriggerPlan          = "plan"          // загрузка плана подвода (перезапись снимка с PlanMsk)
)

// JournalEvent — одна запись единого журнала событий данных (append-only).
//
// DocTS — время ИЗ документа (метка формирования выгрузки ЛК / дата плана), НЕ
// время загрузки. По нему меряется актуальность дислокации (гард загрузки плана).
// CreatedAt — когда факт записан на сервере (МСК, из clock.Now()). Detail — сырой
// jsonb (разбивка по терминалам, имя файла, счётчики) для статус-панели.
type JournalEvent struct {
	ID        int64
	EventType string
	Source    string
	Trigger   string // что вызвало обновление снимка (см. Trigger*); пусто для старых строк
	Actor     string
	DocTS     *LocalTime
	Detail    json.RawMessage
	CreatedAt LocalTime
}
