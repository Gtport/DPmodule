package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/Gtport/DPmodule/internal/domain"
)

// vagonKeys — имена полей источника, которые вообще умеет читать jsonVagon (теги
// `json`). Берутся рефлексией, а не списком: добавят поле в модель — переходник
// подхватит его сам, без правки в двух местах.
var vagonKeys = func() map[string]bool {
	t := reflect.TypeOf(jsonVagon{})
	keys := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("json"); tag != "" && tag != "-" {
			keys[tag] = true
		}
	}
	return keys
}()

// ParseRows разбирает ТАБЛИЧНЫЙ ответ дислокации (head — имена полей АСОУП, body —
// строки значений) в те же записи, что и обычный JSON-фид. Так отвечает личный
// кабинет РЖД на опрос готовности отчёта: данные там уже полные, поэтому роботу
// не нужно скачивать xlsx (ручной приём файлов это не отменяет).
//
// Значения кабинета совпадают по формату с фидом АСУ (даты
// «2026-07-12T17:01:00.000», вес в килограммах строкой, коды строками, пустое —
// null), поэтому строка перекладывается в jsonVagon и идёт через тот же convert:
// одна доменная модель, одни правила, ничего специфичного для ЛК.
//
// Битые строки пропускаются молча — как и в остальных парсерах (формат стабилен,
// а терять весь срез из-за одной строки нельзя).
func (p *JSONParser) ParseRows(head []string, body [][]json.RawMessage) ([]domain.Dislocation, error) {
	if len(head) == 0 {
		return nil, fmt.Errorf("табличный ответ без заголовка")
	}

	// Индексы нужных колонок: имя поля → позиция в строке. Лишние 100+ колонок
	// (наименования, координаты, тарифы) не трогаем вовсе.
	type col struct {
		name string
		idx  int
	}
	cols := make([]col, 0, len(vagonKeys))
	seen := make(map[string]bool, len(vagonKeys))
	for i, name := range head {
		if vagonKeys[name] && !seen[name] {
			cols = append(cols, col{name: name, idx: i})
			seen[name] = true
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("в заголовке нет ни одного известного поля (первое: %q)", head[0])
	}

	records := make([]domain.Dislocation, 0, len(body))
	var buf bytes.Buffer
	for _, row := range body {
		// Собираем объект строки из сырых значений: они уже валидный JSON, поэтому
		// это склейка байтов, без разбора каждого значения по отдельности.
		buf.Reset()
		buf.WriteByte('{')
		first := true
		for _, c := range cols {
			if c.idx >= len(row) || len(row[c.idx]) == 0 || bytes.Equal(row[c.idx], []byte("null")) {
				continue // пустое поле — то же самое, что отсутствующее
			}
			if !first {
				buf.WriteByte(',')
			}
			first = false
			buf.WriteByte('"')
			buf.WriteString(c.name)
			buf.WriteString(`":`)
			buf.Write(row[c.idx])
		}
		buf.WriteByte('}')

		var v jsonVagon
		if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
			continue
		}
		r := p.convert(v)
		if r.Vagon == "" {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}
