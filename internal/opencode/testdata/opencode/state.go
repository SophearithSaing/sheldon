package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// readFixtureState loads only explicitly enabled disposable native-fixture persistence.
func readFixtureState(name string, target any) {
	root := os.Getenv("SHELDON_NATIVE_STATE")
	if root == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(root, name+".json"))
	if err == nil {
		if json.Unmarshal(data, target) != nil {
			os.Exit(4)
		}
	} else if !os.IsNotExist(err) {
		os.Exit(4)
	}
}

// writeFixtureState saves test-owned native state separately from Sheldon conversation files.
func writeFixtureState(name string, value any) {
	root := os.Getenv("SHELDON_NATIVE_STATE")
	if root == "" {
		return
	}
	if os.MkdirAll(root, 0700) != nil {
		os.Exit(4)
	}
	data, err := json.Marshal(value)
	if err != nil {
		os.Exit(4)
	}
	temporary := filepath.Join(root, name+".tmp")
	if os.WriteFile(temporary, data, 0600) != nil || os.Rename(temporary, filepath.Join(root, name+".json")) != nil {
		os.Exit(4)
	}
}
