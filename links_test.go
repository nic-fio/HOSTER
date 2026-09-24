package main

import (
	"reflect"
	"testing"
)

func TestLoadLinks(t *testing.T) {
	p := writeTemp(t, "links", `# commento
https://a.test/1
https://a.test/1

  https://b.test/2
# altra nota
https://a.test/1
`)
	got, err := LoadLinks(p)
	if err != nil {
		t.Fatalf("LoadLinks: %v", err)
	}
	want := []string{"https://a.test/1", "https://b.test/2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadLinks = %v, want %v", got, want)
	}
}
