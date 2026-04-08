package data

import (
	"fmt"
	"testing"
)

// Compile-time interface assertions.
var (
	_ DataStore     = (*Store)(nil)
	_ ViewLister    = (*Store)(nil)
	_ VirtualLister = (*Store)(nil)
	_ ViewLister    = mockViewLister{}
)

// mockViewLister verifies the ViewLister interface exists and is implementable.
type mockViewLister struct{}

func (m mockViewLister) IsView(string) bool { return false }

func TestViewListerInterface(t *testing.T) {
	// Compile-time check: mockViewLister satisfies ViewLister.
	var _ ViewLister = mockViewLister{}
}

func ExampleIsSafeIdentifier() {
	fmt.Println(IsSafeIdentifier("users"))
	fmt.Println(IsSafeIdentifier("my_table"))
	fmt.Println(IsSafeIdentifier("Robert'); DROP TABLE--"))
	fmt.Println(IsSafeIdentifier(""))
	// Output:
	// true
	// true
	// false
	// false
}
