// Package scriptruntime executes sandboxed embedded scripts.
package scriptruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	Logf        func(format string, args ...any)
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
	openSafeOS(L)
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)

	var output []string
	L.SetGlobal("print", L.NewFunction(func(state *lua.LState) int {
		parts := make([]string, 0, state.GetTop())
		for i := 1; i <= state.GetTop(); i++ {
			parts = append(parts, state.Get(i).String())
		}
		line := strings.Join(parts, "\t")
		output = append(output, line)
		if opts.Logf != nil {
			opts.Logf("print: %s", line)
		}
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
					if opts.Logf != nil {
						opts.Logf("tool %s argument error: %v", toolName, err)
					}
					state.RaiseError("%v", err)
					return 0
				}
				if opts.Logf != nil {
					opts.Logf("tool %s args: %s", toolName, prettyLogText(mustJSON(args)))
				}
				result, err := opts.ToolClient.CallToolText(ctx, toolName, args)
				if err != nil {
					if opts.Logf != nil {
						opts.Logf("tool %s error: %v", toolName, err)
					}
					state.RaiseError("%v", err)
					return 0
				}
				if opts.Logf != nil {
					opts.Logf("tool %s result: %s", toolName, prettyLogText(result))
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

	jsonTable := L.NewTable()
	L.SetField(jsonTable, "encode", L.NewFunction(func(state *lua.LState) int {
		val, err := luaValueToGo(state.CheckAny(1))
		if err != nil {
			state.RaiseError("%v", err)
			return 0
		}
		b, err := json.Marshal(val)
		if err != nil {
			state.RaiseError("%v", err)
			return 0
		}
		state.Push(lua.LString(b))
		return 1
	}))
	L.SetField(jsonTable, "decode", L.NewFunction(func(state *lua.LState) int {
		s := state.CheckString(1)
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			state.RaiseError("%v", err)
			return 0
		}
		state.Push(goToLuaValue(state, parsed))
		return 1
	}))
	L.SetGlobal("json", jsonTable)

	if err := L.DoString(script); err != nil {
		if opts.Logf != nil {
			opts.Logf("error: %v", err)
		}
		return "", err
	}
	return strings.TrimSpace(strings.Join(output, "\n")), nil
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func prettyLogText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		if pretty, err := json.MarshalIndent(decoded, "", "  "); err == nil {
			return string(pretty)
		}
	}
	return text
}

// ValidateLua parses a Lua script without executing it.
func ValidateLua(script string) error {
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("script is empty")
	}
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})
	defer L.Close()
	if _, err := L.LoadString(script); err != nil {
		return err
	}
	return nil
}

func openSafeOS(L *lua.LState) {
	osTable := L.NewTable()
	L.SetField(osTable, "date", L.NewFunction(func(state *lua.LState) int {
		format := "%c"
		if state.GetTop() >= 1 {
			format = state.CheckString(1)
		}
		when := time.Now()
		if state.GetTop() >= 2 {
			when = time.Unix(int64(state.CheckNumber(2)), 0)
		}
		useUTC := strings.HasPrefix(format, "!")
		if useUTC {
			format = strings.TrimPrefix(format, "!")
			when = when.UTC()
		} else {
			when = when.In(time.Local)
		}
		if format == "*t" {
			table := state.NewTable()
			state.SetField(table, "year", lua.LNumber(when.Year()))
			state.SetField(table, "month", lua.LNumber(int(when.Month())))
			state.SetField(table, "day", lua.LNumber(when.Day()))
			state.SetField(table, "hour", lua.LNumber(when.Hour()))
			state.SetField(table, "min", lua.LNumber(when.Minute()))
			state.SetField(table, "sec", lua.LNumber(when.Second()))
			state.SetField(table, "wday", lua.LNumber(int(when.Weekday())+1))
			state.SetField(table, "yday", lua.LNumber(when.YearDay()))
			state.SetField(table, "isdst", lua.LFalse)
			state.Push(table)
			return 1
		}
		formatted, err := luaDateFormat(when, format)
		if err != nil {
			state.RaiseError("%v", err)
			return 0
		}
		state.Push(lua.LString(formatted))
		return 1
	}))
	L.SetField(osTable, "time", L.NewFunction(func(state *lua.LState) int {
		state.Push(lua.LNumber(time.Now().Unix()))
		return 1
	}))
	L.SetField(osTable, "difftime", L.NewFunction(func(state *lua.LState) int {
		t2 := float64(state.CheckNumber(1))
		t1 := float64(state.CheckNumber(2))
		state.Push(lua.LNumber(t2 - t1))
		return 1
	}))
	L.SetGlobal("os", osTable)
}

func luaDateFormat(when time.Time, format string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		ch := format[i]
		if ch != '%' {
			b.WriteByte(ch)
			continue
		}
		i++
		if i >= len(format) {
			return "", fmt.Errorf("invalid os.date format %q", format)
		}
		switch format[i] {
		case '%':
			b.WriteByte('%')
		case 'Y':
			b.WriteString(when.Format("2006"))
		case 'y':
			b.WriteString(when.Format("06"))
		case 'm':
			b.WriteString(when.Format("01"))
		case 'd':
			b.WriteString(when.Format("02"))
		case 'H':
			b.WriteString(when.Format("15"))
		case 'I':
			b.WriteString(when.Format("03"))
		case 'M':
			b.WriteString(when.Format("04"))
		case 'S':
			b.WriteString(when.Format("05"))
		case 'p':
			b.WriteString(when.Format("PM"))
		case 'a':
			b.WriteString(when.Format("Mon"))
		case 'A':
			b.WriteString(when.Format("Monday"))
		case 'b':
			b.WriteString(when.Format("Jan"))
		case 'B':
			b.WriteString(when.Format("January"))
		case 'j':
			fmt.Fprintf(&b, "%03d", when.YearDay())
		case 'w':
			fmt.Fprintf(&b, "%d", int(when.Weekday()))
		case 'Z':
			b.WriteString(when.Format("MST"))
		case 'c':
			b.WriteString(when.Format(time.ANSIC))
		default:
			return "", fmt.Errorf("unsupported os.date format %%%c", format[i])
		}
	}
	return b.String(), nil
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
