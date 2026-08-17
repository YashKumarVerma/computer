package inmemory

import (
	"errors"
	"testing"

	"github.com/yashkumarverma/computer/sdk"
)

func TestInMemory(t *testing.T) {
	s, _ := New(nil)

	_ = s.SetString("s", "hi")
	_ = s.SetInt("i", 42)
	_ = s.SetBool("b", true)

	if v, err := s.GetString("s"); err != nil || v != "hi" {
		t.Fatalf("GetString = %q, %v", v, err)
	}
	if v, err := s.GetInt("i"); err != nil || v != 42 {
		t.Fatalf("GetInt = %d, %v", v, err)
	}
	if v, err := s.GetBool("b"); err != nil || v != true {
		t.Fatalf("GetBool = %v, %v", v, err)
	}

	// missing key
	if _, err := s.GetString("nope"); !errors.Is(err, sdk.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// wrong type: "i" holds an int, ask for string
	if _, err := s.GetString("i"); !errors.Is(err, sdk.ErrWrongType) {
		t.Fatalf("want ErrWrongType, got %v", err)
	}
}
