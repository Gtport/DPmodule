package domain

// ReportPreset — сохранённый клиентский вариант отчётной формы (перенос gtport
// «Подход Марис»: там фильтр клиентов был зашит в кнопку фронта, здесь — строка
// справочника report_preset, правится в Админе). Карточки на странице «Справки
// и отчёты» генерятся из пресетов: исчез пресет — исчезла карточка.
type ReportPreset struct {
	ID        int64  `json:"id"`
	Report    string `json:"report"`     // форма, к которой пресет ('podhod')
	Name      string `json:"name"`       // подпись карточки («Марис»)
	Clients   string `json:"clients"`    // фильтр клиентов, разделитель '|' (формат gtport)
	SortOrder int    `json:"sort_order"` // порядок карточек
	Enabled   bool   `json:"enabled"`
}
