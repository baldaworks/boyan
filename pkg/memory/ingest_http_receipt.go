package memory

func IngestReceiptFromOutcome(outcome IngestOutcome) (IngestReceipt, error) {
	if err := outcome.Validate(); err != nil {
		return IngestReceipt{}, err
	}
	receipt := IngestReceipt{
		SchemaVersion:  IngestReceiptSchemaVersionV1,
		ExportID:       outcome.ExportID,
		Disposition:    outcome.Disposition,
		Projection:     outcome.Projection,
		OperationIDs:   append([]string(nil), outcome.OperationIDs...),
		ScopeVersion:   outcome.ScopeVersion,
		ScopeChangeSeq: outcome.ScopeChangeSeq,
	}
	if len(receipt.OperationIDs) == 1 {
		receipt.OperationID = receipt.OperationIDs[0]
	}
	if err := receipt.Validate(); err != nil {
		return IngestReceipt{}, err
	}
	return receipt, nil
}
