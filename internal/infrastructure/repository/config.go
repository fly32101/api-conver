package repository

import "api-conver/internal/config"

// ConfigRepository provides access to configuration
type ConfigRepository struct{}

func NewConfigRepository() *ConfigRepository {
	return &ConfigRepository{}
}

// GetAliasConfig returns the configuration for a given alias
func (r *ConfigRepository) GetAliasConfig(alias string) *config.AliasConfig {
	return config.GetAliasConfig(alias)
}

// IsValidAlias checks if an alias exists in configuration
func (r *ConfigRepository) IsValidAlias(alias string) bool {
	return config.IsValidAlias(alias)
}

// GetDefaultModel returns the default model from configuration
func (r *ConfigRepository) GetDefaultModel() string {
	return config.Get().Defaults.Port
}

// GetAliasDefaultModel returns the default model for an alias, or global default
func (r *ConfigRepository) GetAliasDefaultModel(alias string) string {
	if cfg := r.GetAliasConfig(alias); cfg != nil && cfg.DefaultModel != "" {
		return cfg.DefaultModel
	}
	return "tstars2.0"
}
