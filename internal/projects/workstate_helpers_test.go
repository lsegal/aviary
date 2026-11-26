package projects

import (
	"strconv"
	"strings"
)

// testWorkTarget is the project the single-project work state helpers below
// key their records under.
const testWorkTarget = "o/r"

// loadWorkState reads the work state at path keyed by issue number, for
// tests whose engine watches a single project.
func loadWorkState(path string) (map[int]workState, error) {
	scoped, err := loadScopedWorkState(path, nil)
	if err != nil {
		return nil, err
	}
	result := make(map[int]workState, len(scoped))
	for key, work := range scoped {
		number, err := strconv.Atoi(key[strings.LastIndexByte(key, '#')+1:])
		if err != nil {
			return nil, err
		}
		result[number] = work
	}
	return result, nil
}

// saveWorkState writes state as testWorkTarget's job records.
func saveWorkState(path string, state map[int]workState) error {
	scoped := make(map[string]workState, len(state))
	for number, work := range state {
		scoped[testWorkTarget+"#"+strconv.Itoa(number)] = work
	}
	return saveScopedWorkState(path, scoped, nil)
}
