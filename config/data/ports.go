package data

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/junfuchang/superflare/config/model"
)

var ErrPortRevisionConflict = errors.New("port settings have changed since they were loaded")

func LoadPortBindings() (model.Ports, error) {
	filePath, err := portsConfigPath()
	if err != nil {
		return model.Ports{}, err
	}
	raw, err := readFileCached(filePath, func() ([]byte, error) { return readPortBindingsFile(filePath) })
	if err != nil {
		return model.Ports{}, err
	}
	return LoadPortBindingsFromRaw(raw)
}

func readPortBindingsFile(filePath string) ([]byte, error) {
	exists, err := pathExists(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat ports config failed: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("ports config is missing")
	}
	return readFile(filePath)
}

func LoadPortBindingsFromRaw(raw []byte) (model.Ports, error) {
	var result model.Ports
	if err := yaml.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("parse ports raw failed: %w", err)
	}
	if err := validateLoadedPortBindings(result.Items); err != nil {
		return result, err
	}
	result.Items = normalizePortBindings(result.Items)
	return result, nil
}

func SavePortBindings(ports model.Ports) error {
	return withConfigWriteLock(func() error {
		filePath, err := portsConfigPath()
		if err != nil {
			return err
		}
		_, err = savePortBindingsLocked(filePath, ports.Items)
		return err
	})
}

func savePortBindingsLocked(filePath string, items []model.PortBinding) (string, error) {
	out, err := yaml.Marshal(model.Ports{Items: normalizePortBindings(items)})
	if err != nil {
		return "", fmt.Errorf("marshal ports failed: %w", err)
	}
	if err := saveFileLocked(filePath, out); err != nil {
		return "", fmt.Errorf("save ports failed: %w", err)
	}
	invalidateFileCachePath(filePath)
	return portBindingsRevision(out), nil
}

func portBindingsRevision(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func UpdatePortRemarks(items []model.PortBinding) error {
	return SavePortBindings(model.Ports{Items: items})
}

func UpdatePortRemarksWithRevision(items []model.PortBinding, expected string, includeHidden bool) (revision string, err error) {
	if expected == "" {
		return "", ErrPortRevisionConflict
	}
	err = withConfigWriteLock(func() error {
		filePath, err := portsConfigPath()
		if err != nil {
			return err
		}
		// Check the file on disk, including changes made outside this process.
		raw, err := readPortBindingsFile(filePath)
		if err != nil {
			return fmt.Errorf("ports config error: %w", err)
		}
		if expected != portBindingsRevision(raw) {
			return ErrPortRevisionConflict
		}
		if !includeHidden {
			current, err := LoadPortBindingsFromRaw(raw)
			if err != nil {
				return fmt.Errorf("ports config error: %w", err)
			}
			bindings := portBindingMap(model.Ports{Items: items})
			for _, item := range current.Items {
				if _, present := bindings[portBindingKey(item.Protocol, item.Port)]; item.Hidden && !present {
					items = append(items, item)
				}
			}
		}
		revision, err = savePortBindingsLocked(filePath, items)
		return err
	})
	return revision, err
}

func UpdatePortCollectionEnabled(enabled bool) error {
	if err := EnsureAppConfigExists(); err != nil {
		return err
	}
	return updateLockedAppConfig("config", func(options model.Application) (model.Application, error) {
		options.DisablePortCollection = !enabled
		return options, nil
	})
}

func GetPortBindingMapWithError() (map[string]model.PortBinding, error) {
	ports, err := LoadPortBindings()
	if err != nil {
		return map[string]model.PortBinding{}, err
	}
	return portBindingMap(ports), nil
}

func GetPortBindingMapWithRevision() (map[string]model.PortBinding, string, error) {
	filePath, err := portsConfigPath()
	if err != nil {
		return nil, "", err
	}
	raw, err := readPortBindingsFile(filePath)
	if err != nil {
		return nil, "", err
	}
	ports, err := LoadPortBindingsFromRaw(raw)
	if err != nil {
		return nil, "", err
	}
	return portBindingMap(ports), portBindingsRevision(raw), nil
}

func portBindingMap(ports model.Ports) map[string]model.PortBinding {
	bindings := make(map[string]model.PortBinding, len(ports.Items))
	for _, item := range ports.Items {
		if item.Port <= 0 {
			continue
		}
		item.Protocol = normalizePortProtocol(item.Protocol)
		item.Remark = strings.TrimSpace(item.Remark)
		bindings[portBindingKey(item.Protocol, item.Port)] = item
	}
	return bindings
}

func GetPortRemarkMapWithError() (map[string]string, error) {
	bindings, err := GetPortBindingMapWithError()
	if err != nil {
		return map[string]string{}, err
	}
	remarks := make(map[string]string, len(bindings))
	for key, item := range bindings {
		remarks[key] = strings.TrimSpace(item.Remark)
	}
	return remarks, nil
}

func normalizePortBindings(items []model.PortBinding) []model.PortBinding {
	unique := map[string]model.PortBinding{}
	for _, item := range items {
		if item.Port <= 0 || item.Port > 65535 {
			continue
		}
		item.Protocol = normalizePortProtocol(item.Protocol)
		item.Remark = strings.TrimSpace(item.Remark)
		if item.Remark == "" && !item.Hidden {
			continue
		}
		key := portBindingKey(item.Protocol, item.Port)
		unique[key] = item
	}
	result := make([]model.PortBinding, 0, len(unique))
	for _, item := range unique {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port == result[j].Port {
			return result[i].Protocol < result[j].Protocol
		}
		return result[i].Port < result[j].Port
	})
	return result
}

func validateLoadedPortBindings(items []model.PortBinding) error {
	for index, item := range items {
		if item.Port <= 0 || item.Port > 65535 {
			return fmt.Errorf("invalid port binding at row %d: port %d is out of range", index+1, item.Port)
		}
		protocol := strings.ToLower(strings.TrimSpace(item.Protocol))
		switch protocol {
		case "", "tcp", "udp":
		default:
			return fmt.Errorf("invalid port binding at row %d: protocol %q is not supported", index+1, item.Protocol)
		}
	}
	return nil
}

func normalizePortProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "udp":
		return "udp"
	default:
		return "tcp"
	}
}

func portBindingKey(protocol string, port int) string {
	return normalizePortProtocol(protocol) + ":" + strconv.Itoa(port)
}

func GetPortsConfigPath() string {
	return getPortsConfigPath()
}

func portsConfigPath() (string, error) {
	rootDir, err := configRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "ports.yaml"), nil
}

func getPortsConfigPath() string {
	rootDir, err := configRootDir()
	if err != nil {
		return filepath.Join(".", "ports.yaml")
	}
	return filepath.Join(rootDir, "ports.yaml")
}

func GetPortsConfigPathErr() (string, error) {
	return portsConfigPath()
}
