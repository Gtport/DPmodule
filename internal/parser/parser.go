// Package parser переносит парсеры входных данных GTport (JSON из АСУ, Excel из
// ЛК, ответ запроса 601) в чистые преобразования «сырой вход → доменные модели».
// Парсеры не зависят от БД и репозиториев: на вход — байты/файл, на выход —
// []domain.Dislocation / []domain.VagonOperation. Обогащение (Stage 1–4) и
// персистентность — отдельные слои.
package parser

import (
	"fmt"
	"strings"
	"time"
)

// generateDeterministicID — детерминированный ID записи дислокации по ключевым
// полям: "vagon/codeStationNach/DD.MM.YYYY". Стабилен между загрузками одного
// рейса. При отсутствии ключа — временный ID (temp_<unixnano>).
func generateDeterministicID(vagon, codeStationNach string, dateNach *time.Time) string {
	if vagon == "" || codeStationNach == "" || dateNach == nil || dateNach.IsZero() {
		return fmt.Sprintf("temp_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%s/%s/%s", vagon, codeStationNach, dateNach.Format("02.01.2006"))
}

// safeSubstring безопасно извлекает подстроку [start; start+length) без паник.
func safeSubstring(s string, start, length int) string {
	if start >= len(s) {
		return ""
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// normalizeCyrillic заменяет латинские омоглифы на кириллические (частая беда
// выгрузок АСУ: «C» латинская вместо «С» кириллической и т.п.).
func normalizeCyrillic(text string) string {
	if text == "" {
		return text
	}
	replacements := map[rune]rune{
		'A': 'А', 'B': 'В', 'C': 'С', 'E': 'Е', 'H': 'Н', 'K': 'К', 'M': 'М',
		'O': 'О', 'P': 'Р', 'T': 'Т', 'X': 'Х', 'Y': 'У',
		'a': 'а', 'c': 'с', 'e': 'е', 'o': 'о', 'p': 'р', 'x': 'х', 'y': 'у',
	}
	return strings.Map(func(r rune) rune {
		if replacement, exists := replacements[r]; exists {
			return replacement
		}
		return r
	}, text)
}
