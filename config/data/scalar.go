package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ReadConfigScalar lets lifecycle scripts use the same parsers as the server.
// Missing files or keys return an empty value; unreadable or malformed files
// return an error so callers cannot mistake a parse failure for a missing key.
func ReadConfigScalar(format, filePath, key string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch format {
	case "env":
		envs, err := LoadEnvFileFromContent(raw)
		if err != nil {
			return "", fmt.Errorf("invalid env configuration")
		}
		return envs.Section("").Key(key).Value(), nil
	case "yaml":
		// Decode only the requested value so unrelated structured settings are allowed.
		var values map[string]yaml.Node
		if err := yaml.Unmarshal(raw, &values); err != nil {
			return "", fmt.Errorf("invalid YAML configuration")
		}
		var value string
		if node, ok := values[key]; ok {
			if err := node.Decode(&value); err != nil {
				return "", fmt.Errorf("invalid YAML configuration value")
			}
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported configuration format %q", format)
	}
}
