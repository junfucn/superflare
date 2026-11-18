package data

import (
	"fmt"
	"log"

	"gopkg.in/yaml.v2"
	yamlv3 "gopkg.in/yaml.v3"

	"github.com/junfuchang/superflare/config/model"
)

func initAppConfigLocked(filePath string) (result model.Application, err error) {
	result, out, err := loadDefaultAppConfig()
	if err != nil {
		return result, err
	}

	if err := saveFileLocked(filePath, out); err != nil {
		log.Println("init default app config failed")
		return result, fmt.Errorf("init default app config failed: %w", err)
	}

	return result, nil
}

func saveAppConfigToYamlFile(name string, result model.Application) error {
	return saveAppConfigToYamlFileWith(name, result, saveFile)
}

func saveAppConfigToYamlFileLocked(name string, result model.Application) error {
	return saveAppConfigToYamlFileWith(name, result, saveFileLocked)
}

func saveAppConfigToYamlFileWith(name string, result model.Application, save func(string, []byte) error) error {
	out, err := yaml.Marshal(result)
	if err != nil {
		log.Println("marshal app config failed")
		return fmt.Errorf("marshal app config failed: %w", err)
	}

	filePath, err := configPath(name)
	if err != nil {
		return err
	}
	if err := save(filePath, out); err != nil {
		log.Println("save app config failed")
		return fmt.Errorf("save app config failed: %w", err)
	}
	invalidateFileCachePath(filePath)
	return nil
}

func loadAppConfigFromYaml(name string) (model.Application, error) {
	return loadAppConfigSnapshot(name, false)
}

func loadAppConfigSnapshot(name string, normalized bool) (model.Application, error) {
	result := model.Application{ShowFavorites: true}
	filePath, err := configPath(name)
	if err != nil {
		return result, err
	}

	exists, err := pathExists(filePath)
	if err != nil {
		return result, fmt.Errorf("stat config %s failed: %w", name, err)
	}
	if !exists {
		return result, fmt.Errorf("config %s is missing", name)
	}
	kind := "application"
	if normalized {
		kind = "settings"
	}
	return readParsedFileCached(filePath, kind, func(raw []byte) (model.Application, error) {
		if err := yaml.Unmarshal(raw, &result); err != nil {
			return result, fmt.Errorf("parse config %s failed: %w", name, err)
		}
		if normalized {
			return normalizeSettingsOptions(result)
		}
		return result, nil
	})
}

func LoadAppConfigFromRaw(raw []byte) (model.Application, error) {
	result := model.Application{ShowFavorites: true}
	if err := yaml.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("parse config raw failed: %w", err)
	}
	if err := validateSettingsOptions(result); err != nil {
		return result, err
	}
	if err := validateLoginCredentialValues(result.LoginUser, result.LoginPass, "config.yml"); err != nil {
		return result, err
	}
	return result, nil
}

func upsertRawConfigFields(raw []byte, updates ...yaml.MapItem) ([]byte, error) {
	// Preserve scalar text and unknown settings when upgrading an older backup.
	var document yamlv3.Node
	if err := yamlv3.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		document = yamlv3.Node{Kind: yamlv3.DocumentNode, Content: []*yamlv3.Node{{Kind: yamlv3.MappingNode, Tag: "!!map"}}}
	}
	fields := document.Content[0]
	if fields.Kind == yamlv3.ScalarNode && fields.Tag == "!!null" {
		*fields = yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map"}
	}
	if fields.Kind != yamlv3.MappingNode {
		return nil, fmt.Errorf("config must be a YAML mapping")
	}
	for _, update := range updates {
		var key, value yamlv3.Node
		if err := key.Encode(update.Key); err != nil {
			return nil, err
		}
		if err := value.Encode(update.Value); err != nil {
			return nil, err
		}
		found := false
		for index := 0; index < len(fields.Content); index += 2 {
			field := fields.Content[index]
			if field.Kind == key.Kind && field.Tag == key.Tag && field.Value == key.Value {
				previous := fields.Content[index+1]
				next := value
				next.HeadComment, next.LineComment, next.FootComment = previous.HeadComment, previous.LineComment, previous.FootComment
				preserveRawConfigAliases(&document, previous)
				fields.Content[index+1] = &next
				found = true
			}
		}
		if !found {
			fields.Content = append(fields.Content, &key, &value)
		}
	}
	return yamlv3.Marshal(&document)
}

func preserveRawConfigAliases(node, replaced *yamlv3.Node) {
	if replaced.Anchor == "" {
		return
	}
	if node.Kind == yamlv3.AliasNode && node.Alias == replaced {
		// Keep other fields that referenced an empty setting at their original value.
		next := *replaced
		next.Anchor = ""
		next.HeadComment, next.LineComment, next.FootComment = node.HeadComment, node.LineComment, node.FootComment
		*node = next
	}
	for _, child := range node.Content {
		preserveRawConfigAliases(child, replaced)
	}
}
