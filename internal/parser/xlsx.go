// Package parser — разбор входных выгрузок АСУ РЖД (SPV4664): JSON и Excel «ЛК».
//
// Здесь пока только тонкая обёртка над excelize, фиксирующая зависимость в стеке
// шаблона. Полные парсеры (parse_lk, parse_json) переносятся из GTport отдельным
// шагом: оба кормят одну доменную модель и дают один набор полей.
package parser

import "github.com/xuri/excelize/v2"

// OpenXLSX открывает .xlsx-файл выгрузки ЛК. Вызывающий обязан закрыть файл (f.Close()).
func OpenXLSX(path string) (*excelize.File, error) {
	return excelize.OpenFile(path)
}
