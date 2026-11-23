package scriptruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlwaysErrors(t *testing.T) {
	for script, want := range map[string]bool{
		`error("exec is not available")`:                                true,
		"-- stub\nerror('aborted: ' .. 'no exec')":                      true,
		`local msg = "no exec" error(msg)`:                              true,
		`return error("no")`:                                            true,
		`print("ok")`:                                                   false,
		`local r = tool.exec({command = "git status"}) error(r.stdout)`: false,
		`if not tool.exec then error("no exec") end print("ok")`:        false,
		`local ok = pcall(function() error("x") end) print(ok)`:         false,
		`obj:error("x")`:                                                false,
		`print("before") error("after")`:                                false,
		`error(`:                                                        false,
	} {
		assert.Equal(t, want, AlwaysErrors(script), script)
	}
}
