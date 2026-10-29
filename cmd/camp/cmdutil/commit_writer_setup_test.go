package cmdutil

import "testing"

func TestCommitWriterInput(t *testing.T) {
	for _, value := range []string{"", " ", "\n\t"} {
		if nonemptyWriterValue(value) == nil {
			t.Errorf("accepted empty input %q", value)
		}
	}
	for _, value := range []string{"ob commit --print-session-id", "printf 'my message'", "WRITER_MODE=commit ./writer | cat"} {
		if err := nonemptyWriterValue(value); err != nil {
			t.Errorf("rejected shell command %q: %v", value, err)
		}
	}
}
