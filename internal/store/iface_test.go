package store

import "testing"

// mockViewLister verifies the ViewLister interface exists and is implementable.
type mockViewLister struct{}

func (m mockViewLister) IsView(string) bool { return false }

func TestViewListerInterface(t *testing.T) {
	var _ ViewLister = mockViewLister{}
}
