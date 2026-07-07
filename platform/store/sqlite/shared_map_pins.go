package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/bpfman/bpfman/kernel"
)

// prepareSharedMapPinStatements prepares SQL statements for shared map
// pin operations.
func (s *sqliteStore) prepareSharedMapPinStatements(ctx context.Context) error {
	var err error

	s.stmtSaveSharedMapPin, err = s.db.PrepareContext(ctx,
		`INSERT OR IGNORE INTO shared_map_pins (map_name, program_id) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare SaveSharedMapPin: %w", err)
	}

	s.stmtDeleteSharedMapPins, err = s.db.PrepareContext(ctx,
		`DELETE FROM shared_map_pins WHERE program_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare DeleteSharedMapPins: %w", err)
	}

	s.stmtListSharedMapsByProgram, err = s.db.PrepareContext(ctx,
		`SELECT map_name FROM shared_map_pins WHERE program_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare ListSharedMapsByProgram: %w", err)
	}

	s.stmtCountSharedMapRefs, err = s.db.PrepareContext(ctx,
		`SELECT COUNT(*) FROM shared_map_pins WHERE map_name = ?`)
	if err != nil {
		return fmt.Errorf("prepare CountSharedMapRefs: %w", err)
	}

	s.stmtListReferencedSharedMaps, err = s.db.PrepareContext(ctx,
		`SELECT DISTINCT map_name FROM shared_map_pins`)
	if err != nil {
		return fmt.Errorf("prepare ListReferencedSharedMaps: %w", err)
	}

	return nil
}

// SaveSharedMapPins records that the given program uses the named
// shared maps.
func (s *sqliteStore) SaveSharedMapPins(ctx context.Context, programID kernel.ProgramID, mapNames []string) error {
	start := time.Now()
	for _, name := range mapNames {
		if _, err := s.stmtSaveSharedMapPin.ExecContext(ctx, name, programID); err != nil {
			s.logger.Debug("sql", "stmt", "SaveSharedMapPin", "args", []any{name, programID}, "duration_ms", msec(time.Since(start)), "error", err)
			return fmt.Errorf("save shared map pin (%s, %d): %w", name, programID, err)
		}
	}
	s.logger.Debug("sql", "stmt", "SaveSharedMapPins", "args", []any{programID, len(mapNames)}, "duration_ms", msec(time.Since(start)))
	return nil
}

// DeleteSharedMapPins removes a program's shared map pin entries and
// returns map names that are no longer referenced by any program.
func (s *sqliteStore) DeleteSharedMapPins(ctx context.Context, programID kernel.ProgramID) ([]string, error) {
	start := time.Now()

	// Step 1: Read this program's shared map names.
	mapNames, err := s.listSharedMapsByProgram(ctx, programID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListSharedMapsByProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	if len(mapNames) == 0 {
		s.logger.Debug("sql", "stmt", "DeleteSharedMapPins", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "maps", 0, "orphaned", 0)
		return nil, nil
	}

	// Step 2: Delete this program's entries.
	if _, err := s.stmtDeleteSharedMapPins.ExecContext(ctx, programID); err != nil {
		s.logger.Debug("sql", "stmt", "DeleteSharedMapPins", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, fmt.Errorf("delete shared map pins for program %d: %w", programID, err)
	}

	// Step 3: Check each map name for remaining references.
	var orphaned []string
	for _, name := range mapNames {
		var count int
		if err := s.stmtCountSharedMapRefs.QueryRowContext(ctx, name).Scan(&count); err != nil {
			return nil, fmt.Errorf("count refs for shared map %q: %w", name, err)
		}
		if count == 0 {
			orphaned = append(orphaned, name)
		}
	}

	s.logger.Debug("sql", "stmt", "DeleteSharedMapPins", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "maps", len(mapNames), "orphaned", len(orphaned))
	return orphaned, nil
}

// ListReferencedSharedMaps returns all shared map names still
// referenced by at least one program.
func (s *sqliteStore) ListReferencedSharedMaps(ctx context.Context) ([]string, error) {
	start := time.Now()
	rows, err := s.stmtListReferencedSharedMaps.QueryContext(ctx)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListReferencedSharedMaps", "duration_ms", msec(time.Since(start)), "error", err)
		return nil, fmt.Errorf("list referenced shared maps: %w", err)
	}

	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan shared map name: %w", err)
		}

		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate referenced shared maps: %w", err)
	}

	s.logger.Debug("sql", "stmt", "ListReferencedSharedMaps", "duration_ms", msec(time.Since(start)), "count", len(names))
	return names, nil
}

// listSharedMapsByProgram returns the shared map names for a program.
func (s *sqliteStore) listSharedMapsByProgram(ctx context.Context, programID kernel.ProgramID) ([]string, error) {
	rows, err := s.stmtListSharedMapsByProgram.QueryContext(ctx, programID)
	if err != nil {
		return nil, fmt.Errorf("list shared maps for program %d: %w", programID, err)
	}

	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan shared map name: %w", err)
		}

		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shared maps for program %d: %w", programID, err)
	}
	return names, nil
}
