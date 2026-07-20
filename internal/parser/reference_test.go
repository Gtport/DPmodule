package parser

import (
	"strings"
	"testing"
)

// Фрагмент боевого ответа attis/reference/update от 2026-07-20 (обрезан до двух
// памяток и минимума вагонов). Первая — все 8 полей вагона, вторая — только
// обязательная пара GET_IN (опциональные поля в JSON отсутствуют целиком).
const refUpdateGolden = `{
  "LAST_UPDATE": "2026-07-20 01:48:37.900",
  "PAMYATKI": [
    {
      "NUMBER_PAMYATKA": "10926",
      "DATE_CREATE": "07-17-2026",
      "OPERATION_TYPE": "уборку",
      "GET_PLACE": "Аттис - 2 путь",
      "NAME_STATION": "Мыс Астафьева",
      "PATH_OWNER_OKPO": "10230304",
      "VAGONS": [
        {
          "NUMBER_VAGON": "60935210",
          "GR_OPERATION_TYPE": "вгр",
          "GET_IN_DATE": "17.07",
          "GET_IN_TIME": "14:30",
          "REPORT_DATE": "17.07",
          "REPORT_TIME": "21:13",
          "GET_OUT_DATE": "17.07",
          "GET_OUT_TIME": "21:30"
        }
      ]
    },
    {
      "NUMBER_PAMYATKA": "10931",
      "DATE_CREATE": "07-18-2026",
      "OPERATION_TYPE": "подачу",
      "GET_PLACE": "22 путь - 77, 78 тыл средние пути (уголь)",
      "NAME_STATION": "МЫС АСТАФЬЕВА",
      "PATH_OWNER_OKPO": "10230304",
      "VAGONS": [
        {
          "NUMBER_VAGON": "63678874",
          "GR_OPERATION_TYPE": "пгр",
          "GET_IN_DATE": "18.07",
          "GET_IN_TIME": "22:10"
        }
      ]
    }
  ]
}`

func TestParseReferenceUpdate_Golden(t *testing.T) {
	got, err := ParseReferenceUpdate([]byte(refUpdateGolden), "attis")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if got.LastUpdate != "2026-07-20 01:48:37.900" {
		t.Errorf("LastUpdate: получили %q", got.LastUpdate)
	}
	if len(got.Pamyatki) != 2 {
		t.Fatalf("памяток: ждали 2, получили %d", len(got.Pamyatki))
	}

	p := got.Pamyatki[0]
	if p.Client != "attis" || p.Number != "10926" {
		t.Errorf("адресация: client=%q number=%q", p.Client, p.Number)
	}
	if p.OperationType != "уборку" || p.NameStation != "Мыс Астафьева" ||
		p.GetPlace != "Аттис - 2 путь" || p.PathOwnerOkpo != "10230304" {
		t.Errorf("шапка памятки разобрана неверно: %+v", p)
	}
	if p.DateCreate == nil || p.DateCreate.String() != "2026-07-17T00:00:00" {
		t.Errorf("DATE_CREATE (MM-DD-YYYY): получили %v", p.DateCreate)
	}
	if len(p.Vagons) != 1 {
		t.Fatalf("вагонов в первой памятке: %d", len(p.Vagons))
	}
	v := p.Vagons[0]
	if v.Vagon != "60935210" || v.GrOperationType != "вгр" {
		t.Errorf("вагон: %+v", v)
	}
	for name, tm := range map[string]string{
		"GetIn":  "2026-07-17T14:30:00",
		"Report": "2026-07-17T21:13:00",
		"GetOut": "2026-07-17T21:30:00",
	} {
		var got string
		switch name {
		case "GetIn":
			got = v.GetIn.String()
		case "Report":
			got = v.Report.String()
		case "GetOut":
			got = v.GetOut.String()
		}
		if got != tm {
			t.Errorf("%s: ждали %s, получили %s", name, tm, got)
		}
	}

	// Вторая памятка: опциональные поля отсутствуют → nil, обязательная пара на месте.
	v2 := got.Pamyatki[1].Vagons[0]
	if v2.GetIn == nil || v2.GetIn.String() != "2026-07-18T22:10:00" {
		t.Errorf("GetIn второй памятки: %v", v2.GetIn)
	}
	if v2.Report != nil || v2.GetOut != nil {
		t.Errorf("опциональные поля должны быть nil: report=%v getOut=%v", v2.Report, v2.GetOut)
	}
}

// Стык лет: памятка создана 2 января — декабрьские времена уходят в прошлый год.
func TestParseReferenceUpdate_YearBoundary_BackToDecember(t *testing.T) {
	raw := `{"LAST_UPDATE":"x","PAMYATKI":[{"NUMBER_PAMYATKA":"1","DATE_CREATE":"01-02-2027",
	  "OPERATION_TYPE":"подачу","GET_PLACE":"","NAME_STATION":"","PATH_OWNER_OKPO":"",
	  "VAGONS":[{"NUMBER_VAGON":"11111111","GR_OPERATION_TYPE":"вгр",
	    "GET_IN_DATE":"30.12","GET_IN_TIME":"23:50","GET_OUT_DATE":"02.01","GET_OUT_TIME":"08:00"}]}]}`
	got, err := ParseReferenceUpdate([]byte(raw), "attis")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	v := got.Pamyatki[0].Vagons[0]
	if v.GetIn.String() != "2026-12-30T23:50:00" {
		t.Errorf("декабрь при январской памятке должен уйти в прошлый год: %s", v.GetIn)
	}
	if v.GetOut.String() != "2027-01-02T08:00:00" {
		t.Errorf("январь при январской памятке остаётся в её году: %s", v.GetOut)
	}
}

// Стык лет в другую сторону: памятка от 30 декабря, уборка уже в январе.
func TestParseReferenceUpdate_YearBoundary_ForwardToJanuary(t *testing.T) {
	raw := `{"LAST_UPDATE":"x","PAMYATKI":[{"NUMBER_PAMYATKA":"2","DATE_CREATE":"12-30-2026",
	  "OPERATION_TYPE":"уборку","GET_PLACE":"","NAME_STATION":"","PATH_OWNER_OKPO":"",
	  "VAGONS":[{"NUMBER_VAGON":"22222222","GR_OPERATION_TYPE":"вгр",
	    "GET_IN_DATE":"30.12","GET_IN_TIME":"10:00","GET_OUT_DATE":"01.01","GET_OUT_TIME":"09:15"}]}]}`
	got, err := ParseReferenceUpdate([]byte(raw), "nmtp")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	v := got.Pamyatki[0].Vagons[0]
	if v.GetIn.String() != "2026-12-30T10:00:00" {
		t.Errorf("GetIn: %s", v.GetIn)
	}
	if v.GetOut.String() != "2027-01-01T09:15:00" {
		t.Errorf("январь при декабрьской памятке должен уйти в следующий год: %s", v.GetOut)
	}
}

// Дата без времени (или наоборот) — нарушение целостности пары, падаем громко.
func TestParseReferenceUpdate_UnpairedDateTime(t *testing.T) {
	raw := `{"LAST_UPDATE":"x","PAMYATKI":[{"NUMBER_PAMYATKA":"3","DATE_CREATE":"07-19-2026",
	  "OPERATION_TYPE":"подачу","GET_PLACE":"","NAME_STATION":"","PATH_OWNER_OKPO":"",
	  "VAGONS":[{"NUMBER_VAGON":"33333333","GR_OPERATION_TYPE":"вгр",
	    "GET_IN_DATE":"19.07","GET_IN_TIME":"10:00","REPORT_DATE":"19.07"}]}]}`
	_, err := ParseReferenceUpdate([]byte(raw), "attis")
	if err == nil || !strings.Contains(err.Error(), "REPORT") {
		t.Fatalf("ждали ошибку про непарный REPORT, получили: %v", err)
	}
}

// DATE_CREATE не в американском формате — ошибка с адресом памятки.
func TestParseReferenceUpdate_BadDateCreate(t *testing.T) {
	raw := `{"LAST_UPDATE":"x","PAMYATKI":[{"NUMBER_PAMYATKA":"4","DATE_CREATE":"17-07-2026",
	  "OPERATION_TYPE":"подачу","GET_PLACE":"","NAME_STATION":"","PATH_OWNER_OKPO":"","VAGONS":[]}]}`
	_, err := ParseReferenceUpdate([]byte(raw), "nmtp")
	if err == nil || !strings.Contains(err.Error(), "nmtp/4") {
		t.Fatalf("ждали ошибку с адресом nmtp/4, получили: %v", err)
	}
}

// Пустой инкремент — не ошибка: курсор есть, памяток нет.
func TestParseReferenceUpdate_Empty(t *testing.T) {
	got, err := ParseReferenceUpdate([]byte(`{"LAST_UPDATE":"2026-07-20 03:00:00.000","PAMYATKI":[]}`), "attis")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(got.Pamyatki) != 0 || got.LastUpdate == "" {
		t.Errorf("пустой инкремент: %+v", got)
	}
}
