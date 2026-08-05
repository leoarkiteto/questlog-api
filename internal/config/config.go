// Package config loads backend configuration from environment variables
// and an optional .env file.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv reads KEY=VALUE pairs from the first existing file in
// paths and sets any variable that isn't already present in the
// environment (existing env vars always win). Missing files are
// ignored. Handles blank lines, whole-line comments, an optional
// "export " prefix, surrounding quotes, and whitespace.
func LoadDotEnv(paths ...string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open %s: %w", p, err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			if key == "" || val == "" {
				continue
			}
			if os.Getenv(key) == "" {
				if err := os.Setenv(key, val); err != nil {
					_ = f.Close()
					return err
				}
			}
		}
		closeErr := f.Close()
		if err := sc.Err(); err != nil {
			return err
		}
		return closeErr
	}
	return nil
}
