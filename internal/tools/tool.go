package tools

import (
	"fmt"
	"path/filepath"
)

// requireUnderCWD returns an error if path is not under workDir.
// Uses filepath.Abs to handle relative paths and walks up to find
// the deepest existing ancestor before calling EvalSymlinks, so it
// works even when the target file doesn't exist yet.
func requireUnderCWD(path, workDir string) error {
	// Resolve workDir to its real absolute path
	cwd, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("unsafe_mode: cannot resolve workdir: %w", err)
	}

	// Resolve path — walk up until we find an existing ancestor
	target, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("unsafe_mode: cannot resolve path: %w", err)
	}
	existing := target
	for existing != "/" && existing != "." {
		if _, err := filepath.EvalSymlinks(existing); err == nil {
			if real, err := filepath.EvalSymlinks(existing); err == nil {
				existing = real
			}
			break
		}
		existing = filepath.Dir(existing)
	}

	if !filepath.HasPrefix(existing, cwd) && existing != cwd {
		return fmt.Errorf("unsafe_mode: path %q is outside session CWD %q — set unsafe_mode=true to allow", path, cwd)
	}
	return nil
}

// helpers shared across tool implementations

func strArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return def
}

func boolArg(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}
