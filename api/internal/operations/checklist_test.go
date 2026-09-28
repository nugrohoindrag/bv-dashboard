package operations

import "testing"

// PRD P0 v2 §12.2: expected result per tipe input.
func TestIsDeviation(t *testing.T) {
	s := func(v string) *string { return &v }
	f := func(v float64) *float64 { return &v }
	min, max := 20.0, 70.0
	cases := []struct {
		name  string
		spec  ChecklistTemplateItem
		value *string
		num   *float64
		want  *bool
	}{
		{"ok", ChecklistTemplateItem{ItemType: "ok_notok_na"}, s("ok"), nil, bp(false)},
		{"not_ok", ChecklistTemplateItem{ItemType: "ok_notok_na"}, s("not_ok"), nil, bp(true)},
		{"na", ChecklistTemplateItem{ItemType: "ok_notok_na"}, s("na"), nil, bp(false)},
		{"yes_no legacy no", ChecklistTemplateItem{ItemType: "yes_no"}, s("no"), nil, bp(true)},
		{"yes_no expected no", ChecklistTemplateItem{ItemType: "yes_no", ExpectedValue: s("no")}, s("no"), nil, bp(false)},
		{"pass", ChecklistTemplateItem{ItemType: "pass_fail"}, s("pass"), nil, bp(false)},
		{"fail", ChecklistTemplateItem{ItemType: "pass_fail"}, s("fail"), nil, bp(true)},
		{"selection tanpa expected", ChecklistTemplateItem{ItemType: "selection"}, s("dirty"), nil, bp(false)},
		{"selection sesuai", ChecklistTemplateItem{ItemType: "selection", ExpectedValue: s("clean, ok")}, s("ok"), nil, bp(false)},
		{"selection menyimpang", ChecklistTemplateItem{ItemType: "selection", ExpectedValue: s("clean")}, s("dirty"), nil, bp(true)},
		{"numeric dalam rentang", ChecklistTemplateItem{ItemType: "numeric", NumericMin: &min, NumericMax: &max}, nil, f(50), bp(false)},
		{"numeric di atas", ChecklistTemplateItem{ItemType: "numeric", NumericMin: &min, NumericMax: &max}, nil, f(85), bp(true)},
		{"numeric di bawah", ChecklistTemplateItem{ItemType: "numeric", NumericMin: &min}, nil, f(10), bp(true)},
		{"text", ChecklistTemplateItem{ItemType: "text"}, nil, nil, nil},
		{"signature", ChecklistTemplateItem{ItemType: "signature"}, nil, nil, nil},
	}
	for _, c := range cases {
		got := isDeviation(c.spec, c.value, c.num)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: got %v want %v", c.name, ptrStr(got), ptrStr(c.want))
		}
	}
}

func TestValidateItemSpec(t *testing.T) {
	s := func(v string) *string { return &v }
	bad := []ChecklistTemplateItem{
		{Label: "a", ItemType: "selection", Options: []ChecklistOption{{Value: "x"}}},
		{Label: "b", ItemType: "selection", Options: []ChecklistOption{{Value: "x"}, {Value: "x"}}},
		{Label: "c", ItemType: "selection", Options: []ChecklistOption{{Value: "x"}, {Value: "y"}}, ExpectedValue: s("z")},
		{Label: "d", ItemType: "pass_fail", ExpectedValue: s("ok")},
		{Label: "e", ItemType: "yes_no", ExpectedValue: s("maybe")},
	}
	for _, it := range bad {
		it := it
		if err := validateItemSpec(&it); err == nil {
			t.Errorf("%s harus ditolak", it.Label)
		}
	}
	good := ChecklistTemplateItem{Label: "ok", ItemType: "selection", Options: []ChecklistOption{{Value: " clean "}, {Value: "dirty", Label: "Kotor"}}, ExpectedValue: s("clean")}
	if err := validateItemSpec(&good); err != nil {
		t.Fatal(err)
	}
	if good.Options[0].Value != "clean" || good.Options[0].Label != "clean" {
		t.Fatalf("normalisasi opsi: %+v", good.Options)
	}
	txt := ChecklistTemplateItem{Label: "t", ItemType: "text", ExpectedValue: s("x"), Options: []ChecklistOption{{Value: "a"}}}
	_ = validateItemSpec(&txt)
	if txt.ExpectedValue != nil || txt.Options != nil {
		t.Fatal("text tidak menyimpan expected/options")
	}
}

func bp(b bool) *bool { return &b }
func ptrStr(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}
