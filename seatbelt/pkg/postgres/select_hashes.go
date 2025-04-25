package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"seatbelt-source-postgres/pkg/config"
)

// FetchSelectHashes executes the SELECT query to get IDs and computed hashes from the database.
func FetchSelectHashes(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (map[int32]int64, error) {
	log.Printf("Fetching hashes via SELECT query for table %s...", cfg.Table.Name)

	// Build the concatenation part of the query dynamically
	var coalesceParts []string
	for _, colName := range cfg.Table.HashColumns {
		// Basic quoting to prevent SQL injection for column names (assuming valid identifiers)
		// A more robust solution might involve checking against actual table schema.
		safeColName := pgx.Identifier{colName}.Sanitize()
		coalesceParts = append(coalesceParts, fmt.Sprintf("COALESCE(%s::text, '')", safeColName))
	}
	concatenationExpression := strings.Join(coalesceParts, " || ")

	// Build the full query
	// Ensure ID column is also quoted safely
	safeIDColumn := pgx.Identifier{cfg.Table.IDColumn}.Sanitize()
	// Schema needs separate handling if used directly, but table name includes it
	// Split schema and table to sanitize properly
	var safeFullTableName string
	parts := strings.SplitN(cfg.Table.Name, ".", 2)
	if len(parts) == 2 {
		// We have schema.table format
		schema := pgx.Identifier{parts[0]}.Sanitize()
		table := pgx.Identifier{parts[1]}.Sanitize()
		safeFullTableName = schema + "." + table
	} else {
		// Just table name, assume public schema
		safeFullTableName = pgx.Identifier{cfg.Table.Name}.Sanitize()
	}

	// Note: Passing schema.table directly to Identifier might not work as expected for quoting.
	// Constructing schema.table quoting manually might be safer if needed.
	// For now, assuming cfg.Table.Name is correctly formatted e.g. `public.data_proof`

	query := fmt.Sprintf(`
		SELECT
			%s, -- ID Column
			hashtextextended((%s), $1) AS computed_hash
		FROM
			%s -- Table Name
	`, safeIDColumn, concatenationExpression, safeFullTableName)

	if cfg.Debug {
		log.Printf("DEBUG: Executing SQL: %s with seed: %d", query, cfg.HashSeed)
	}

	rows, err := pool.Query(ctx, query, cfg.HashSeed)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SELECT query: %w", err)
	}
	defer rows.Close()

	selectHashes := make(map[int32]int64)
	for rows.Next() {
		var id int32
		var hash pgtype.Int8 // Use pgtype to handle potential NULL hash if row exists but hash fails?
		if err := rows.Scan(&id, &hash); err != nil {
			log.Printf("Error scanning row for SELECT hash: %v", err)
			continue // Skip problematic rows
		}

		if hash.Valid {
			selectHashes[id] = hash.Int64
		} else {
			// This case implies hashtextextended returned NULL, which is unlikely unless input was NULL
			// and the function behavior changes, or the ID itself was NULL (which scan should handle).
			log.Printf("Warning: Received NULL computed hash for ID %d from SELECT query.", id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating SELECT hash rows: %w", err)
	}

	log.Printf("Fetched %d hashes via SELECT.", len(selectHashes))
	return selectHashes, nil
}
