package cli_test

import (
	"testing"

	"github.com/CaffeineOgre/media-organizer/internal/cli"
)

func TestRun(t *testing.T) {
	if err := cli.Run(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
