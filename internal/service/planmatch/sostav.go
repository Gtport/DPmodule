package planmatch

import (
	"fmt"
	"strings"
)

// FormatSostav форматирует «Состав» нитки — читаемый список сматченных подгрупп
// вагонов для таблицы плана. Дословный перенос эталона formatTrainStructure
// (gtlogic .../api/plan_handler.go): код = символы 6–8 IndexMain; подгруппа —
// «(кол)-код-Sms Naznach» либо «(кол)-код-Sms GruzpolS → Naznach» (когда груз
// отличается от назначения); разделитель «; », перенос строки после каждой 3-й.
//
// Пустые значения (в матче — unknownFallback) показываем как пусто, как в эталоне.
func FormatSostav(subGroups []SubGroup) string {
	if len(subGroups) == 0 {
		return ""
	}
	parts := make([]string, 0, len(subGroups))
	for _, sg := range subGroups {
		code := "???"
		if len(sg.IndexMain) >= 8 {
			code = sg.IndexMain[5:8]
		}
		sms := blankFallback(sg.Sms1)
		naznach := blankFallback(sg.Naznach)
		gruzpol := blankFallback(sg.GruzpolS)

		var part string
		if gruzpol != "" && gruzpol != naznach {
			part = fmt.Sprintf("(%d)-%s-%s %s → %s", sg.Quantity, code, sms, gruzpol, naznach)
		} else {
			part = fmt.Sprintf("(%d)-%s-%s %s", sg.Quantity, code, sms, naznach)
		}
		parts = append(parts, part)
	}

	var b strings.Builder
	for i, part := range parts {
		b.WriteString(part)
		if i < len(parts)-1 {
			b.WriteString("; ")
			if (i+1)%3 == 0 {
				b.WriteString("\n")
			}
		} else {
			b.WriteString(";")
		}
	}
	return b.String()
}

// StationOperOf — станция текущей операции нитки для столбца «Дислокация»:
// первый непустой StationOper среди подгрупп победившей агрегации.
func StationOperOf(subGroups []SubGroup) string {
	for _, sg := range subGroups {
		if s := blankFallback(sg.StationOper); s != "" {
			return s
		}
	}
	return ""
}

// blankFallback превращает служебное значение unknownFallback обратно в пусто —
// для отображения (в матче пустые поля заменяются на unknownFallback).
func blankFallback(s string) string {
	if s == unknownFallback {
		return ""
	}
	return s
}
