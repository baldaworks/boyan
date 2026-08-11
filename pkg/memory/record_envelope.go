package memory

// RecordEnvelope is the deterministic protobuf-wire envelope for a portable
// memory record. Payload encoding remains owned by the corresponding contract.
type RecordEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	RecordType    string `json:"record_type"`
	PayloadSHA256 string `json:"payload_sha256"`
	Payload       []byte `json:"payload"`
}
