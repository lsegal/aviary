package agents

import (
	"encoding/json"
	"fmt"
	"strings"
)

// modelPath is a parsed doctor.modelsJSON expression: the route from a JSON
// document a CLI printed to the model ids buried in it. It exists because the
// CLIs that can answer "what models do you take" mostly answer in JSON rather
// than in the one-model-per-line a plain modelPattern reads -- a catalog
// command prints an object of records, and an agent that speaks a stdio
// protocol prints one JSON-RPC response per line.
//
// The grammar is the smallest one those two shapes need: dotted field names,
// with `[]` on a name whose value is an array to walk every element, and
// `[key=value]` to walk only the elements whose field equals value, for a
// catalog that ships rows it hides from its own picker.
type modelPath []modelStep

// modelStep is one field of the route, and how to descend through its value.
type modelStep struct {
	// field is the object key this step reads, empty for a step that only
	// walks the value it was handed.
	field string
	// each says the value is an array to walk rather than to read.
	each bool
	// filterKey and filterValue narrow that walk to the elements whose field
	// equals the value, when the step named one.
	filterKey   string
	filterValue string
}

// parseModelPath reads the expression, reporting the first thing wrong with it
// rather than quietly extracting nothing: a path that matches nothing and a
// path that is misspelled produce the same empty list, and only one of them is
// worth telling the author about.
func parseModelPath(expr string) (modelPath, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, fmt.Errorf("path is empty")
	}
	path := make(modelPath, 0, 4)
	for _, segment := range strings.Split(trimmed, ".") {
		step, err := parseModelStep(segment)
		if err != nil {
			return nil, err
		}
		path = append(path, step)
	}
	return path, nil
}

// parseModelStep reads one dotted segment, with its optional array selector.
func parseModelStep(segment string) (modelStep, error) {
	name, selector, found := strings.Cut(strings.TrimSpace(segment), "[")
	step := modelStep{field: strings.TrimSpace(name)}
	if !found {
		if step.field == "" {
			return step, fmt.Errorf("segment %q names no field", segment)
		}
		return step, nil
	}
	if !strings.HasSuffix(selector, "]") {
		return step, fmt.Errorf("segment %q is missing its closing bracket", segment)
	}
	step.each = true
	filter := strings.TrimSpace(strings.TrimSuffix(selector, "]"))
	if filter == "" {
		return step, nil
	}
	key, value, ok := strings.Cut(filter, "=")
	if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return step, fmt.Errorf("segment %q wants a key=value filter", segment)
	}
	step.filterKey, step.filterValue = strings.TrimSpace(key), strings.TrimSpace(value)
	return step, nil
}

// extract walks the document and collects every string the path ends on. A
// branch that does not match the path is skipped rather than reported: one
// JSON-RPC line out of ten carries the model list, and the other nine are not
// errors.
func (p modelPath) extract(value any) []string {
	if len(p) == 0 {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return []string{strings.TrimSpace(text)}
		}
		return nil
	}
	step, rest := p[0], p[1:]
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	next, ok := object[step.field]
	if !ok {
		return nil
	}
	if !step.each {
		return rest.extract(next)
	}
	elements, ok := next.([]any)
	if !ok {
		return nil
	}
	models := make([]string, 0, len(elements))
	for _, element := range elements {
		if !step.selects(element) {
			continue
		}
		models = append(models, rest.extract(element)...)
	}
	return models
}

// selects reports whether a filtered step keeps this element. An element that
// is not an object, or whose field is not the value named, is dropped.
func (step modelStep) selects(element any) bool {
	if step.filterKey == "" {
		return true
	}
	object, ok := element.(map[string]any)
	if !ok {
		return false
	}
	text, ok := object[step.filterKey].(string)
	return ok && text == step.filterValue
}

// modelsFromJSON reads the ids the path names out of a probe's output. The
// whole output is tried as one document first, for a command that prints a
// catalog, and each line separately after that, for an agent that answers in
// JSON-RPC and prints a response per line among its own logging.
func modelsFromJSON(expr, output string) []string {
	path, err := parseModelPath(expr)
	if err != nil {
		return nil
	}
	var document any
	if err := json.Unmarshal([]byte(output), &document); err == nil {
		if models := path.extract(document); len(models) > 0 {
			return models
		}
	}
	models := make([]string, 0, 16)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
			continue
		}
		models = append(models, path.extract(value)...)
	}
	return models
}
