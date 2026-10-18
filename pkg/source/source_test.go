package source

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse("3;0;12;\n")
	if err != nil || !slices.Equal(got, []int{3, 0, 12}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := Parse("3;x;"); err == nil {
		t.Error("want an error for a non-number")
	}
}

func TestRead(t *testing.T) {
	frames := make(chan []int, 4)
	if err := Read(strings.NewReader("1;2;\n3;4;\n"), frames); err != nil {
		t.Fatal(err)
	}
	var all [][]int
	for f := range frames {
		all = append(all, f)
	}
	if len(all) != 2 || !slices.Equal(all[1], []int{3, 4}) {
		t.Errorf("got %v", all)
	}
}
