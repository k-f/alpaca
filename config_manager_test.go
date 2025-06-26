package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Helper function to create a temporary config directory for tests
func tempConfigDir(t *testing.T) (string, func()) {
	t.Helper()
	tempDir, err := ioutil.TempDir("", "test_config_manager_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Override os.UserConfigDir for the duration of the test
	// This is a bit of a hack. A better way would be to inject the base path into NewConfigManager.
	// For now, we'll rely on creating the expected subdirectory within our tempDir.
	// And ensure our NewConfigManager uses a path relative to some controllable base if possible.
	// Since NewConfigManager uses os.UserConfigDir directly, we'll create the expected path inside tempDir.

	expectedAppConfigDir := filepath.Join(tempDir, configDirName)
	// Pre-create this so NewConfigManager doesn't fail if it tries to use the real one.
	// The test will focus on the file *within* this emulated structure.

	// To make NewConfigManager use this tempDir, we would ideally pass the base path to it.
	// Let's modify NewConfigManager slightly for testability or use a test-specific constructor.
	// For now, we'll create a dummy config file at the location NewConfigManager would create it,
	// assuming we could control its root.

	// Simplification: configManager will create files in its default location.
	// We will test its functions by creating a manager, then inspecting the actual file it creates/modifies.
	// The cleanup function will remove the actual config directory created by the tests.
	// This is less isolated but tests the real paths.

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("Failed to get user config dir for cleanup path: %v", err)
	}
	appConfigDir := filepath.Join(userConfigDir, configDirName)
	configFile := filepath.Join(appConfigDir, configFileName)

	// Store original state if file exists
	var originalContent []byte
	var originalExists bool
	if _, err := os.Stat(configFile); err == nil {
		originalExists = true
		originalContent, err = ioutil.ReadFile(configFile)
		if err != nil {
			t.Fatalf("Failed to read existing original config file: %v", err)
		}
	}

	// Cleanup function
	cleanup := func() {
		if originalExists {
			if err := ioutil.WriteFile(configFile, originalContent, 0600); err != nil {
				t.Errorf("Failed to restore original config file: %v", err)
			}
		} else {
			os.RemoveAll(appConfigDir) // Remove the whole directory if it was created by test
		}
	}

	// Ensure the directory is clean before the test if it wasn't original.
	if !originalExists {
		os.RemoveAll(appConfigDir)
	}


	return configFile, cleanup
}


func TestNewConfigManager_CreatesDefault(t *testing.T) {
	_, cleanup := tempConfigDir(t)
	defer cleanup()

	// Ensure no config file exists initially by removing the directory
	userConfigDir, _ := os.UserConfigDir()
	appConfigPath := filepath.Join(userConfigDir, configDirName)
	os.RemoveAll(appConfigPath)


	cm, err := NewConfigManager()
	if err != nil {
		t.Fatalf("NewConfigManager() error = %v", err)
	}
	if cm == nil {
		t.Fatal("NewConfigManager() returned nil")
	}

	// Check if file was created
	if _, err := os.Stat(cm.configFilePath); os.IsNotExist(err) {
		t.Errorf("NewConfigManager did not create default config file at %s", cm.configFilePath)
	}

	// Check default content
	config := cm.GetConfig()
	if len(config.AllowAlways) != 0 || len(config.DenyAlways) != 0 {
		t.Errorf("Default config is not empty. Allow: %v, Deny: %v", config.AllowAlways, config.DenyAlways)
	}
}

func TestConfigManager_LoadSave(t *testing.T) {
	cfgFile, cleanup := tempConfigDir(t)
	defer cleanup()

	os.RemoveAll(filepath.Dir(cfgFile)) // Clean start

	cm, _ := NewConfigManager() // Creates default

	// Modify and save
	testAllow := []string{"*.example.com"}
	testDeny := []string{"blocked.com"}
	testUpstream := "http://proxy.example.com:8080"

	cm.config.AllowAlways = testAllow
	cm.config.DenyAlways = testDeny
	cm.config.UpstreamProxy = testUpstream
	if err := cm.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// Create a new manager to load the saved config
	cm2, err := NewConfigManager()
	if err != nil {
		t.Fatalf("NewConfigManager() for load test error = %v", err)
	}

	loadedConfig := cm2.GetConfig()
	if !reflect.DeepEqual(loadedConfig.AllowAlways, testAllow) {
		t.Errorf("Loaded AllowAlways = %v, want %v", loadedConfig.AllowAlways, testAllow)
	}
	if !reflect.DeepEqual(loadedConfig.DenyAlways, testDeny) {
		t.Errorf("Loaded DenyAlways = %v, want %v", loadedConfig.DenyAlways, testDeny)
	}
	if loadedConfig.UpstreamProxy != testUpstream {
		t.Errorf("Loaded UpstreamProxy = %s, want %s", loadedConfig.UpstreamProxy, testUpstream)
	}
}

func TestConfigManager_AddRules(t *testing.T) {
	cfgFile, cleanup := tempConfigDir(t)
	defer cleanup()
	os.RemoveAll(filepath.Dir(cfgFile)) // Clean start


	cm, _ := NewConfigManager()

	// Add allow rule
	allowRule1 := "allow.com"
	if err := cm.AddAllowRule(allowRule1); err != nil {
		t.Fatalf("AddAllowRule(%s) error = %v", allowRule1, err)
	}
	config := cm.GetConfig()
	if len(config.AllowAlways) != 1 || config.AllowAlways[0] != allowRule1 {
		t.Errorf("AllowAlways after AddAllowRule = %v, want [%s]", config.AllowAlways, allowRule1)
	}

	// Add duplicate allow rule (should not change)
	if err := cm.AddAllowRule(allowRule1); err != nil {
		t.Fatalf("AddAllowRule duplicate error = %v", err)
	}
	config = cm.GetConfig()
	if len(config.AllowAlways) != 1 {
		t.Errorf("AllowAlways after duplicate add = %v, want 1 item", config.AllowAlways)
	}

	// Add deny rule
	denyRule1 := "deny.com"
	if err := cm.AddDenyRule(denyRule1); err != nil {
		t.Fatalf("AddDenyRule(%s) error = %v", denyRule1, err)
	}
	config = cm.GetConfig()
	if len(config.DenyAlways) != 1 || config.DenyAlways[0] != denyRule1 {
		t.Errorf("DenyAlways after AddDenyRule = %v, want [%s]", config.DenyAlways, denyRule1)
	}

	// Check if saved to file by loading with new manager
	cm2, _ := NewConfigManager()
	config2 := cm2.GetConfig()
	if len(config2.AllowAlways) != 1 || config2.AllowAlways[0] != allowRule1 {
		t.Errorf("AllowAlways from file = %v, want [%s]", config2.AllowAlways, allowRule1)
	}
	if len(config2.DenyAlways) != 1 || config2.DenyAlways[0] != denyRule1 {
		t.Errorf("DenyAlways from file = %v, want [%s]", config2.DenyAlways, denyRule1)
	}
}

func TestConfigManager_UpstreamProxy(t *testing.T) {
	cfgFile, cleanup := tempConfigDir(t)
	defer cleanup()
	os.RemoveAll(filepath.Dir(cfgFile)) // Clean start

	cm, _ := NewConfigManager()

	proxyURL := "http://myproxy.com:3128"
	if err := cm.SetUpstreamProxy(proxyURL); err != nil {
		t.Fatalf("SetUpstreamProxy() error = %v", err)
	}

	if cm.GetUpstreamProxy() != proxyURL {
		t.Errorf("GetUpstreamProxy() = %s, want %s", cm.GetUpstreamProxy(), proxyURL)
	}

	// Check if saved
	cm2, _ := NewConfigManager()
	if cm2.GetUpstreamProxy() != proxyURL {
		t.Errorf("GetUpstreamProxy() from file = %s, want %s", cm2.GetUpstreamProxy(), proxyURL)
	}

	// Clear proxy
	if err := cm.SetUpstreamProxy(""); err != nil {
		t.Fatalf("SetUpstreamProxy(\"\") error = %v", err)
	}
	if cm.GetUpstreamProxy() != "" {
		t.Errorf("GetUpstreamProxy() after clear = %s, want \"\"", cm.GetUpstreamProxy())
	}
	cm3, _ := NewConfigManager()
	if cm3.GetUpstreamProxy() != "" {
		t.Errorf("GetUpstreamProxy() from file after clear = %s, want \"\"", cm3.GetUpstreamProxy())
	}
}
