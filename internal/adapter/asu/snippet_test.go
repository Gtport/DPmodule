package asu

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Обрез длинного русского текста не должен давать битый UTF-8 (стресс-тест
// 200 вагонов: битый last_error валил UPDATE и заявка ретраилась вечно).
func TestSnippetValidUTF8(t *testing.T) {
	long := strings.Repeat("объектов спр", 30) // 200-й байт попадает в середину руны
	got := snippet([]byte(long))
	if !utf8.ValidString(got) {
		t.Fatalf("snippet вернул невалидный UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("обрезанный текст должен оканчиваться …: %q", got)
	}
	if short := snippet([]byte("короткий")); short != "короткий" {
		t.Fatalf("короткий текст не должен обрезаться: %q", short)
	}
}
