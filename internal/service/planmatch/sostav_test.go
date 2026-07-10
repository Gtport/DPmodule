package planmatch

import "testing"

func TestFormatSostav(t *testing.T) {
	tests := []struct {
		name string
		sg   []SubGroup
		want string
	}{
		{
			name: "пусто",
			sg:   nil,
			want: "",
		},
		{
			name: "одна подгруппа, груз = назначение",
			sg: []SubGroup{
				{IndexMain: "9379-771-9857", Sms1: "9857", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 45},
			},
			want: "(45)-771-9857 АЭ;",
		},
		{
			name: "груз ≠ назначение → стрелка",
			sg: []SubGroup{
				{IndexMain: "9379-771-9857", Sms1: "9857", Naznach: "АЭ", GruzpolS: "ГУТ-2", Quantity: 12},
			},
			want: "(12)-771-9857 ГУТ-2 → АЭ;",
		},
		{
			name: "две подгруппы через «; »",
			sg: []SubGroup{
				{IndexMain: "9379-771-9857", Sms1: "9857", Naznach: "АЭ", GruzpolS: "ГУТ-2", Quantity: 12},
				{IndexMain: "8630-777-9857", Sms1: "9857", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 5},
			},
			want: "(12)-771-9857 ГУТ-2 → АЭ; (5)-777-9857 АЭ;",
		},
		{
			name: "перенос строки после 3-й подгруппы",
			sg: []SubGroup{
				{IndexMain: "1111-111-0001", Sms1: "0001", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 1},
				{IndexMain: "2222-222-0002", Sms1: "0002", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 2},
				{IndexMain: "3333-333-0003", Sms1: "0003", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 3},
				{IndexMain: "4444-444-0004", Sms1: "0004", Naznach: "АЭ", GruzpolS: "АЭ", Quantity: 4},
			},
			want: "(1)-111-0001 АЭ; (2)-222-0002 АЭ; (3)-333-0003 АЭ; \n(4)-444-0004 АЭ;",
		},
		{
			name: "короткий индекс и пустые (UNKNOWN) поля",
			sg: []SubGroup{
				{IndexMain: "123", Sms1: unknownFallback, Naznach: unknownFallback, GruzpolS: unknownFallback, Quantity: 1},
			},
			want: "(1)-???- ;",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatSostav(tt.sg); got != tt.want {
				t.Errorf("FormatSostav()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestStationOperOf(t *testing.T) {
	sg := []SubGroup{
		{StationOper: unknownFallback},
		{StationOper: "НАХОДКА-ВОСТОЧНАЯ"},
		{StationOper: "ПАРТИЗАНСК"},
	}
	if got := StationOperOf(sg); got != "НАХОДКА-ВОСТОЧНАЯ" {
		t.Errorf("StationOperOf() = %q, want НАХОДКА-ВОСТОЧНАЯ", got)
	}
	if got := StationOperOf([]SubGroup{{StationOper: unknownFallback}}); got != "" {
		t.Errorf("StationOperOf(only UNKNOWN) = %q, want empty", got)
	}
}
