package app

import (
	"reflect"
	"testing"
)

// Every feature panel reaches the window through its host; a panel added to
// App without wiring it in wirePanels would panic on first use.
func TestEveryPanelHasItsHost(t *testing.T) {
	window, _ := newTestApp(t)
	headless, err := newHeadlessApp(testControlConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]*App{"window": window, "headless": headless} {
		v := reflect.ValueOf(a).Elem()
		panels := 0
		for i := range v.NumField() {
			f := v.Field(i)
			if f.Kind() != reflect.Struct {
				continue
			}
			host := f.FieldByName("host")
			if !host.IsValid() || host.Kind() != reflect.Interface {
				continue
			}
			panels++
			if host.IsNil() {
				t.Errorf("%s app: panel %s has no host; add it to wirePanels", name, v.Type().Field(i).Name)
			}
		}
		if panels < 20 {
			t.Errorf("%s app: found %d panels, want at least 20", name, panels)
		}
	}
}
