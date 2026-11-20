package whiteboard

import "testing"

func TestArchitectureLibraryUsesStandardElementsAndFreshBindings(t *testing.T) {
	lib := BuiltinLibrary()
	if len(lib) < 10 {
		t.Fatal("architecture examples missing")
	}
	for _, item := range lib {
		s := NewScene()
		s.Elements = item.Elements
		s.Files = item.Files
		if err := s.Validate(); err != nil {
			t.Fatalf("%s: %v", item.Name, err)
		}
		for _, e := range s.Elements {
			if e.Kind == "cylinder" || e.Kind == "cloud" {
				t.Fatal("private shape leaked into exchange model")
			}
		}
		target := NewScene()
		ids, err := target.InsertLibrary(item, Point{100, 100})
		if err != nil || len(ids) == 0 {
			t.Fatalf("insert %s: %v", item.Name, err)
		}
		for _, e := range target.Elements {
			if s.ElementByID(e.ID) != nil {
				t.Fatal("library reused IDs")
			}
		}
	}
	encoded, err := EncodeLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeLibrary(encoded)
	if err != nil || len(loaded) != len(lib) {
		t.Fatalf("library exchange: %v", err)
	}
}
