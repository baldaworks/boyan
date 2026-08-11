package standaloneruntime

func recordStopError(target *error, err error) {
	if err != nil && *target == nil {
		*target = err
	}
}
