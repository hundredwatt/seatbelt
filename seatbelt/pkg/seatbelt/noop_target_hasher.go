package seatbelt

type NoopTargetHasher struct {
	SourceHasher SourceHasher
}

func (h *NoopTargetHasher) TransformSourceToCommon(row []interface{}) (string, error) {
	return h.SourceHasher.FormatSource(row)
}

func (h *NoopTargetHasher) TransformTargetToCommon(row []interface{}) (string, error) {
	return h.SourceHasher.FormatSource(row)
}

func (h *NoopTargetHasher) TargetHash(data string) RowHash {
	return h.SourceHasher.SourceHash(data)
}
