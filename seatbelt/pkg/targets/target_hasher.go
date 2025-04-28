package targets

// TargetHasher defines the interface for target-specific data transformation and hashing.
type TargetHasher interface {
	// Transform prepares the row data into a string suitable for target hashing.
	// It takes the raw row values parsed from Postgres replication (map[columnName]value)
	// and the list of column names required by this target (from Columns()).
	// It should return the concatenated string based on target-specific formatting rules.
	Transform(row map[string]interface{}) (string, error)

	// Hash computes the hash of the transformed data string using the target's algorithm.
	// It might require a seed depending on the algorithm.
	Hash(data string, seed int64) uint64
}
