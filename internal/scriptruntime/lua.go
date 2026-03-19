// Package scriptruntime executes sandboxed embedded scripts.
package scriptruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/lsegal/aviary/internal/agent"
)

// Environment exposes runtime context to embedded scripts.
type Environment struct {
	AgentID   string
	SessionID string
	TaskID    string
	JobID     string
}

// Options configures a sandboxed Lua script execution.
type Options struct {
	ToolClient  agent.ToolClient
	Environment Environment
}

// RunLua executes a Lua script with a sandboxed global environment.
func RunLua(ctx context.Context, script string, opts Options) (string, error) {
	if strings.TrimSpace(script) == "" {
		return "", fmt.Errorf("script is empty")
	}
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})
	defer L.Close()

	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)

	var output []string
	L.SetGlobal("print", L.NewFunction(func(state *lua.LState) int {
		parts := make([]string, 0, state.GetTop())
		for i := 1; i <= state.GetTop(); i++ {
			parts = append(parts, state.Get(i).String())
		}
		output = append(output, strings.Join(parts, "\t"))
		return 0
	}))

	L.SetGlobal("environment", goToLuaValue(L, map[string]any{
		"agent_id":   opts.Environment.AgentID,
		"session_id": opts.Environment.SessionID,
		"task_id":    opts.Environment.TaskID,
		"job_id":     opts.Environment.JobID,
	}))

	toolTable := L.NewTable()
	if opts.ToolClient != nil {
		tools, err := opts.ToolClient.ListTools(ctx)
		if err != nil {
			return "", err
		}
		for _, tool := range tools {
			toolName := tool.Name
			L.SetField(toolTable, toolName, L.NewFunction(func(state *lua.LState) int {
				args, err := luaArgsToMap(state)
				if err != nil {
					state.RaiseError("%v", err)
					return 0
				}
				result, err := opts.ToolClient.CallToolText(ctx, toolName, args)
				if err != nil {
					state.RaiseError("%v", err)
					return 0
				}
				state.Push(decodeToolResult(state, result))
				return 1
			}))
		}
	}
	meta := L.NewTable()
	L.SetField(meta, "__index", L.NewFunction(func(state *lua.LState) int {
		name := state.CheckString(2)
		state.RaiseError("tool %q is not enabled for this agent", name)
		return 0
	}))
	L.SetMetatable(toolTable, meta)
	L.SetGlobal("tool", toolTable)

	if err := L.DoString(script); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(output, "\n")), nil
}

func luaArgsToMap(L *lua.LState) (map[string]any, error) {
	switch L.GetTop() {
	case 0:
		return map[string]any{}, nil
	case 1:
		table, ok := L.Get(1).(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("tool functions accept at most one table argument")
		}
		value, err := luaValueToGo(table)
		if err != nil {
			return nil, err
		}
		if value == nil {
			return map[string]any{}, nil
		}
		mapped, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool functions require an object-like table argument")
		}
		return mapped, nil
	default:
		return nil, fmt.Errorf("tool functions accept at most one table argument")
	}
}

func luaValueToGo(value lua.LValue) (any, error) {
	switch v := value.(type) {
	case lua.LBool:
		return bool(v), nil
	case lua.LString:
		return string(v), nil
	case lua.LNumber:
		return float64(v), nil
	case *lua.LNilType:
		return nil, nil
	case *lua.LTable:
		if isArrayTable(v) {
			out := make([]any, 0, v.Len())
			for i := 1; i <= v.Len(); i++ {
				item, err := luaValueToGo(v.RawGetInt(i))
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			return out, nil
		}
		out := map[string]any{}
		var convErr error
		v.ForEach(func(key, val lua.LValue) {
			if convErr != nil {
				return
			}
			keyStr, ok := key.(lua.LString)
			if !ok {
				convErr = fmt.Errorf("tool argument objects require string keys")
				return
			}
			converted, err := luaValueToGo(val)
			if err != nil {
				convErr = err
				return
			}
			out[string(keyStr)] = converted
		})
		if convErr != nil {
			return nil, convErr
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported lua value type %s", value.Type().String())
	}
}

func isArrayTable(table *lua.LTable) bool {
	if table.Len() == 0 {
		return false
	}
	isArray := true
	table.ForEach(func(key, _ lua.LValue) {
		if !isArray {
			return
		}
		number, ok := key.(lua.LNumber)
		if !ok {
			isArray = false
			return
		}
		index := int(number)
		if float64(number) != float64(index) || index < 1 || index > table.Len() {
			isArray = false
		}
	})
	return isArray
}

func decodeToolResult(L *lua.LState, text string) lua.LValue {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		return goToLuaValue(L, parsed)
	}
	return lua.LString(text)
}

func goToLuaValue(L *lua.LState, value any) lua.LValue {
	switch v := value.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(v)
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case float32:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case int32:
		return lua.LNumber(v)
	case uint:
		return lua.LNumber(v)
	case uint64:
		return lua.LNumber(v)
	case uint32:
		return lua.LNumber(v)
	case []any:
		table := L.NewTable()
		for _, item := range v {
			table.Append(goToLuaValue(L, item))
		}
		return table
	case map[string]any:
		table := L.NewTable()
		for key, item := range v {
			L.SetField(table, key, goToLuaValue(L, item))
		}
		return table
	default:
		return lua.LString(fmt.Sprintf("%v", value))
	}
}
