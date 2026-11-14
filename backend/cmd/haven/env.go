package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// loadEnv reads local configuration without executing shell code or printing
// values. Explicit process environment takes precedence over the local file.
func loadEnv(path string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	line := 0
	valid := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		parts := strings.SplitN(text, "=", 2)
		if len(parts) != 2 || !valid.MatchString(strings.TrimSpace(parts[0])) {
			return fmt.Errorf("invalid configuration at line %d", line)
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			var e error
			value, e = strconv.Unquote(value)
			if e != nil {
				return fmt.Errorf("invalid quoted configuration at line %d", line)
			}
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if e := os.Setenv(key, value); e != nil {
				return fmt.Errorf("configuration failed at line %d", line)
			}
		}
	}
	return scanner.Err()
}
