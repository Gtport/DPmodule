package domain

// AdminTable — запись реестра редактируемых справочников (dpport.list_tables)
// c определённым ключом строки для универсального CRUD админ-редактора.
type AdminTable struct {
	Name   string `json:"name"`    // имя таблицы в схеме dpport
	NameRu string `json:"name_ru"` // подпись для владельца
	PK     string `json:"pk"`      // колонка-идентификатор строки: id, если есть, иначе одноколоночный PK
}

// AdminColumn — колонка редактируемой таблицы: фронт строит по ней и грид,
// и динамическую форму добавления/правки.
type AdminColumn struct {
	Name     string `json:"name"`
	Label    string `json:"label"`    // русская подпись (COMMENT ON COLUMN); пусто → фронт покажет Name
	Kind     string `json:"kind"`     // number | text | boolean (из типа Postgres)
	Required bool   `json:"required"` // NOT NULL без DEFAULT — поле обязательно в форме
	PK       bool   `json:"pk"`       // колонка-идентификатор (в форме не правится)
}

// AdminRow — строка редактируемой таблицы в динамическом виде (колонка → значение).
type AdminRow = map[string]any
