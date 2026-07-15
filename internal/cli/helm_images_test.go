package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/airgapper/internal/cli"
)

func TestHelmImagesCmd_MissingRequiredFlags(t *testing.T) {
	root := cli.NewRootCmd("test", "abc", "2026-01-01")
	root.SetArgs([]string{"helm", "images", "--config", "nonexistent.yaml"})
	err := root.Execute()
	assert.Error(t, err)
}
