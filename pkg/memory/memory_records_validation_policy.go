package memory

func validateSensitivityRetention(sensitivity Sensitivity, retention RetentionClass) error {
	if sensitivity != SensitivityStandard && sensitivity != SensitivitySensitive {
		return invalidDerived("unsupported memory sensitivity")
	}

	if retention != RetentionClassStandard && retention != RetentionClassEphemeral {
		return invalidDerived("unsupported memory retention")
	}

	return nil
}
