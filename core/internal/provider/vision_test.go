package provider

import "testing"

func TestSplitOCRSeparatesTextFromScene(t *testing.T) {
	raw := "На столе ноутбук и кружка, рядом окно.\nTEXT: Дедлайн 14 августа\nвстреча в 15:00"
	desc, ocr := splitOCR(raw)
	if desc != "На столе ноутбук и кружка, рядом окно." {
		t.Fatalf("description = %q", desc)
	}
	if ocr != "Дедлайн 14 августа\nвстреча в 15:00" {
		t.Fatalf("ocr = %q", ocr)
	}
}

func TestSplitOCRWithoutTextMarker(t *testing.T) {
	desc, ocr := splitOCR("Просто комната, текста нет.")
	if desc != "Просто комната, текста нет." {
		t.Fatalf("description = %q", desc)
	}
	if ocr != "" {
		t.Fatalf("ocr should be empty, got %q", ocr)
	}
}

func TestSplitOCRIsSafeOnEmptyInput(t *testing.T) {
	desc, ocr := splitOCR("   ")
	if desc != "" || ocr != "" {
		t.Fatalf("expected empty results, got %q / %q", desc, ocr)
	}
}
