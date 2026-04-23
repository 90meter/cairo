package db

// ResolveModel returns the model to use for a given role.
// Resolution order: role.model → config.model → fallback.
// This is the single source of truth for model selection across
// interactive sessions, background tasks, and spawned agents.
func ResolveModel(database *DB, roleName, fallback string) (string, error) {
	// 1. role-specific model
	if roleName != "" {
		roleModel, err := database.Roles.ModelFor(roleName)
		if err != nil {
			return fallback, err
		}
		if roleModel != "" {
			return roleModel, nil
		}
	}

	// 2. global config model
	configModel, err := database.Config.Get("model")
	if err != nil {
		return fallback, err
	}
	if configModel != "" {
		return configModel, nil
	}

	// 3. hardcoded fallback
	return fallback, nil
}
