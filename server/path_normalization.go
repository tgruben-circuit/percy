package server

import (
	"path/filepath"
	"strings"
)

// normalizePathForInput rewrites /private-prefixed macOS paths back to their
// non-/private form when the caller path uses the non-/private alias.
func normalizePathForInput(inputPath, outputPath string) string {
	inputClean := filepath.Clean(inputPath)
	outputClean := filepath.Clean(outputPath)

	if !strings.HasPrefix(outputClean, "/private/") || strings.HasPrefix(inputClean, "/private/") {
		return outputClean
	}

	trimmed := strings.TrimPrefix(outputClean, "/private")
	if trimmed == outputClean {
		return outputClean
	}

	resolvedOutput, err := filepath.EvalSymlinks(outputClean)
	if err != nil {
		return outputClean
	}
	resolvedTrimmed, err := filepath.EvalSymlinks(trimmed)
	if err != nil {
		return outputClean
	}
	if resolvedOutput != resolvedTrimmed {
		return outputClean
	}

	return trimmed
}
