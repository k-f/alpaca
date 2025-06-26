package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	configDirName  = "SecureLLMAgentProxy"
	configFileName = "config.yaml"
)

// Config represents the structure of the configuration file.
type Config struct {
	AllowAlways []string `yaml:"allow_always"`
	DenyAlways  []string `yaml:"deny_always"`
	// UpstreamProxy is the URL of the corporate proxy (e.g., Zscaler)
	UpstreamProxy string `yaml:"upstream_proxy,omitempty"`
}

// ConfigManager handles loading and saving the proxy configuration.
type ConfigManager struct {
	configFilePath string
	config         Config
	mu             sync.RWMutex
}

// NewConfigManager creates a new ConfigManager and loads the initial configuration.
// It determines the appropriate config directory based on the OS.
func NewConfigManager() (*ConfigManager, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user config directory: %w", err)
	}

	appConfigDir := filepath.Join(userConfigDir, configDirName)
	if err := os.MkdirAll(appConfigDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create app config directory %s: %w", appConfigDir, err)
	}

	configFilePath := filepath.Join(appConfigDir, configFileName)

	cm := &ConfigManager{
		configFilePath: configFilePath,
	}

	if err := cm.LoadConfig(); err != nil {
		// If the file doesn't exist, create it with defaults.
		if os.IsNotExist(err) {
			fmt.Printf("Config file not found at %s, creating with defaults.\n", configFilePath)
			cm.config = Config{ // Default empty lists
				AllowAlways: []string{},
				DenyAlways:  []string{},
			}
			if saveErr := cm.SaveConfig(); saveErr != nil {
				return nil, fmt.Errorf("failed to save initial default config: %w", saveErr)
			}
		} else {
			return nil, fmt.Errorf("failed to load initial config: %w", err)
		}
	}
	return cm, nil
}

// LoadConfig loads the configuration from the YAML file.
func (cm *ConfigManager) LoadConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, err := os.Stat(cm.configFilePath); os.IsNotExist(err) {
		return err // Return the os.IsNotExist error specifically
	}

	data, err := ioutil.ReadFile(cm.configFilePath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", cm.configFilePath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config data: %w", err)
	}
	cm.config = config
	return nil
}

// SaveConfig saves the current configuration to the YAML file.
func (cm *ConfigManager) SaveConfig() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	data, err := yaml.Marshal(&cm.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config data: %w", err)
	}

	if err := ioutil.WriteFile(cm.configFilePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", cm.configFilePath, err)
	}
	return nil
}

// GetConfig returns a copy of the current configuration.
func (cm *ConfigManager) GetConfig() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	// Return a copy to prevent modification of the internal state.
	confCopy := cm.config
	confCopy.AllowAlways = append([]string(nil), cm.config.AllowAlways...)
	confCopy.DenyAlways = append([]string(nil), cm.config.DenyAlways...)
	return confCopy
}

// AddAllowRule adds a URL pattern to the allow_always list and saves the config.
func (cm *ConfigManager) AddAllowRule(pattern string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Avoid adding duplicates
	for _, p := range cm.config.AllowAlways {
		if p == pattern {
			return nil // Already exists
		}
	}

	cm.config.AllowAlways = append(cm.config.AllowAlways, pattern)
	return cm.SaveConfig() // SaveConfig is now called without lock, so this is fine
}

// AddDenyRule adds a URL pattern to the deny_always list and saves the config.
func (cm *ConfigManager) AddDenyRule(pattern string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Avoid adding duplicates
	for _, p := range cm.config.DenyAlways {
		if p == pattern {
			return nil // Already exists
		}
	}

	cm.config.DenyAlways = append(cm.config.DenyAlways, pattern)
	return cm.SaveConfig() // SaveConfig is now called without lock, so this is fine
}

// GetUpstreamProxy returns the configured upstream proxy URL.
func (cm *ConfigManager) GetUpstreamProxy() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.UpstreamProxy
}

// SetUpstreamProxy sets the upstream proxy URL and saves the config.
func (cm *ConfigManager) SetUpstreamProxy(proxyURL string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config.UpstreamProxy = proxyURL
	return cm.SaveConfig() // SaveConfig is now called without lock, so this is fine
}
