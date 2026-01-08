package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type AliasConfig struct {
	BaseURL      string `yaml:"base_url"`
	APIKey       string `yaml:"api_key"`
	AuthHeader   string `yaml:"auth_header"`
	AuthPrefix   string `yaml:"auth_prefix"`
	DefaultModel string `yaml:"default_model"`
}

type Config struct {
	Aliases  map[string]AliasConfig `yaml:"aliases"`
	Defaults struct {
		Port string `yaml:"port"`
	} `yaml:"defaults"`
}

var (
	cfg  *Config
	once sync.Once
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	if config.Aliases == nil {
		config.Aliases = make(map[string]AliasConfig)
	}
	if config.Defaults.Port == "" {
		config.Defaults.Port = "8080"
	}

	for name, alias := range config.Aliases {
		if alias.AuthHeader == "" {
			alias.AuthHeader = "Authorization"
			config.Aliases[name] = alias
		}
		if alias.AuthPrefix == "" {
			alias.AuthPrefix = "Bearer"
			config.Aliases[name] = alias
		}
	}

	cfg = &config
	return &config, nil
}

func Get() *Config {
	if cfg != nil {
		return cfg
	}
	once.Do(func() {
		var err error
		cfgPath := Path()
		cfg, err = Load(cfgPath)
		if err != nil {
			cfg = &Config{
				Aliases: make(map[string]AliasConfig),
			}
		}
	})
	return cfg
}

func Reload(path string) (*Config, error) {
	return Load(path)
}

func GetAliasConfig(alias string) *AliasConfig {
	config := Get()
	if aliasConfig, ok := config.Aliases[alias]; ok {
		return &aliasConfig
	}
	return nil
}

func IsValidAlias(alias string) bool {
	config := Get()
	_, ok := config.Aliases[alias]
	return ok
}

func Path() string {
	if path := os.Getenv("CONFIG_PATH"); path != "" {
		return path
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	if path, err := filepath.Abs("config.yaml"); err == nil {
		return path
	}
	return "config.yaml"
}
