// Package config handles onigami's startup configuration. Right now that's
// just loading a .env file so secrets (your Groq API key) live in a file you
// edit, never in the source code or your shell history.
package config

import (
	"bufio"
	"os"
	"strings"
)

// LoadDotenv reads key=value pairs from the given file and sets them as
// environment variables — but only if they aren't already set, so a real
// environment variable always wins over the file.
//
// It is deliberately forgiving: a missing file is NOT an error (you might set
// the key in your shell instead), and blank lines and # comments are skipped.
// This is a minimal parser — no quotes handling, no multiline values — which is
// all we need and keeps the dependency count at zero.
func LoadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no .env file is fine
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blanks and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on the FIRST '=' only, so values may themselves contain '='.
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		// Don't overwrite a variable that's already set in the real environment.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}
