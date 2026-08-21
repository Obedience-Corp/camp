package helpercaller

import "testing"

func TestPureLogic(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("math broke")
	}
}
