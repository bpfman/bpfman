package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// Get retrieves program metadata by program ID.
// Returns platform.ErrRecordNotFound if the program does not exist.
func (s *sqliteStore) Get(ctx context.Context, programID kernel.ProgramID) (bpfman.ProgramRecord, error) {
	start := time.Now()
	row := s.stmtGetProgram.QueryRowContext(ctx, programID)

	prog, err := s.scanProgram(row, programID)
	if errors.Is(err, sql.ErrNoRows) {
		s.logger.Debug("sql", "stmt", "GetProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "rows", 0)
		return bpfman.ProgramRecord{}, platform.ErrRecordNotFound
	}
	if err != nil {
		s.logger.Debug("sql", "stmt", "GetProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return bpfman.ProgramRecord{}, err
	}

	s.logger.Debug("sql", "stmt", "GetProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "rows", 1)

	prog.ProgramID = programID
	return prog, nil
}

// scannedProgram holds the raw column values from a program row scan.
// Both scanProgram and scanProgramFromRows populate this struct, then
// delegate to buildProgramRecord for the shared parsing logic.
type scannedProgram struct {
	programID                                        kernel.ProgramID
	programName, programTypeStr, objectPath, pinPath string
	sourcePath                                       sql.NullString
	attachFunc, globalDataJSON, mapPinPath           sql.NullString
	imageSourceJSON, owner, description              sql.NullString
	license, metadataJSON                            sql.NullString
	mapSetID                                         sql.NullInt64
	gplCompatible                                    int
	createdAtStr                                     string
	updatedAtStr                                     sql.NullString
}

// buildProgramRecord converts scanned column values into a
// ProgramRecord. It handles program type parsing, nullable scalar
// field extraction, JSON unmarshalling, and timestamp parsing.
func buildProgramRecord(sp *scannedProgram) (bpfman.ProgramRecord, error) {
	programType, err := bpfman.ParseProgramType(sp.programTypeStr)
	if err != nil {
		return bpfman.ProgramRecord{}, fmt.Errorf("invalid program type: %q: %w", sp.programTypeStr, err)
	}

	var attachFuncVal string
	var mapOwnerIDPtr *kernel.ProgramID
	var mapPinPathVal string
	if sp.attachFunc.Valid {
		attachFuncVal = sp.attachFunc.String
	}
	if sp.mapSetID.Valid && kernel.ProgramID(sp.mapSetID.Int64) != sp.programID {
		v := kernel.ProgramID(sp.mapSetID.Int64)
		mapOwnerIDPtr = &v
	}
	if sp.mapPinPath.Valid {
		mapPinPathVal = sp.mapPinPath.String
	}

	var globalData map[string][]byte
	var imageURL, imageDigest string
	var imagePullPolicy bpfman.ImagePullPolicy
	var metadata map[string]string
	if sp.globalDataJSON.Valid {
		if err := json.Unmarshal([]byte(sp.globalDataJSON.String), &globalData); err != nil {
			return bpfman.ProgramRecord{}, fmt.Errorf("failed to unmarshal global_data: %w", err)
		}
	}
	if sp.imageSourceJSON.Valid {
		var imgSrc struct {
			URL        string                 `json:"url"`
			Digest     string                 `json:"digest,omitempty"`
			PullPolicy bpfman.ImagePullPolicy `json:"pull_policy"`
		}
		if err := json.Unmarshal([]byte(sp.imageSourceJSON.String), &imgSrc); err != nil {
			return bpfman.ProgramRecord{}, fmt.Errorf("failed to unmarshal image_source: %w", err)
		}
		imageURL = imgSrc.URL
		imageDigest = imgSrc.Digest
		imagePullPolicy = imgSrc.PullPolicy
		if !imagePullPolicy.Valid() {
			return bpfman.ProgramRecord{}, fmt.Errorf("invalid image pull policy in image_source for program %q", sp.programName)
		}
	}
	if sp.metadataJSON.Valid && sp.metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(sp.metadataJSON.String), &metadata); err != nil {
			return bpfman.ProgramRecord{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	createdAt, err := time.Parse(time.RFC3339, sp.createdAtStr)
	if err != nil {
		return bpfman.ProgramRecord{}, fmt.Errorf("invalid created_at timestamp %q: %w", sp.createdAtStr, err)
	}

	// updated_at is nullable in the schema and nil in the
	// in-memory record when the program has never been updated
	// since creation. The pointer encoding keeps "never updated"
	// distinct from "updated at zero time."
	var updatedAt *time.Time
	if sp.updatedAtStr.Valid {
		t, err := time.Parse(time.RFC3339, sp.updatedAtStr.String)
		if err != nil {
			return bpfman.ProgramRecord{}, fmt.Errorf("invalid updated_at timestamp %q: %w", sp.updatedAtStr.String, err)
		}

		updatedAt = &t
	}

	var licenseVal string
	if sp.license.Valid {
		licenseVal = sp.license.String
	}

	prog := bpfman.ProgramRecord{
		ProgramID: sp.programID,
		Load: bpfman.LoadSpec{}.
			WithObjectPath(sp.objectPath).
			WithSourcePath(sp.sourcePath.String).
			WithProgramName(sp.programName).
			WithProgramType(programType).
			WithGlobalData(globalData).
			WithImageProvenance(imageURL, imageDigest, imagePullPolicy).
			WithAttachFunc(attachFuncVal),
		License:       licenseVal,
		GPLCompatible: sp.gplCompatible != 0,
		Handles: bpfman.ProgramHandles{
			PinPath:    bpfman.ProgPinPath(sp.pinPath),
			MapsDir:    bpfman.MapDir(mapPinPathVal),
			MapOwnerID: mapOwnerIDPtr,
		},
		Meta: bpfman.ProgramMeta{
			Name:     sp.programName,
			Metadata: metadata,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if sp.owner.Valid {
		prog.Meta.Owner = sp.owner.String
	}
	if sp.description.Valid {
		prog.Meta.Description = sp.description.String
	}

	return prog, nil
}

// scanProgram scans a single row into a ProgramRecord struct.
func (s *sqliteStore) scanProgram(row *sql.Row, programID kernel.ProgramID) (bpfman.ProgramRecord, error) {
	var sp scannedProgram
	err := row.Scan(
		&sp.programName,
		&sp.programTypeStr,
		&sp.objectPath,
		&sp.sourcePath,
		&sp.pinPath,
		&sp.attachFunc,
		&sp.globalDataJSON,
		&sp.mapSetID,
		&sp.mapPinPath,
		&sp.imageSourceJSON,
		&sp.owner,
		&sp.description,
		&sp.license,
		&sp.gplCompatible,
		&sp.createdAtStr,
		&sp.updatedAtStr,
		&sp.metadataJSON,
	)
	if err != nil {
		return bpfman.ProgramRecord{}, err
	}

	sp.programID = programID
	return buildProgramRecord(&sp)
}

// Save stores program metadata using last-write-wins upsert semantics.
//
// If a row with the same program_id already exists it is overwritten
// rather than rejected. This is necessary because the kernel reuses
// program IDs aggressively after unload, so a collision may simply
// mean the ID was recycled rather than indicating a bug.
//
// On overwrite the original created_at is preserved and updated_at
// is set to the current time so that created_at != updated_at serves
// as a clear signal that the program_id was reused.
//
// For atomicity with other operations, wrap in RunInTransaction.
func (s *sqliteStore) Save(ctx context.Context, programID kernel.ProgramID, metadata bpfman.ProgramRecord) error {
	// Marshal JSON fields
	var globalDataJSON, imageSourceJSON sql.NullString
	if metadata.Load.GlobalData() != nil {
		data, err := json.Marshal(metadata.Load.GlobalData())
		if err != nil {
			return fmt.Errorf("failed to marshal global_data: %w", err)
		}

		globalDataJSON = sql.NullString{String: string(data), Valid: true}
	}
	if metadata.Load.HasImageSource() {
		imgSrc := struct {
			URL        string                 `json:"url"`
			Digest     string                 `json:"digest,omitempty"`
			PullPolicy bpfman.ImagePullPolicy `json:"pull_policy"`
		}{
			URL:        metadata.Load.ImageURL(),
			Digest:     metadata.Load.ImageDigest(),
			PullPolicy: metadata.Load.ImagePullPolicy(),
		}
		data, err := json.Marshal(imgSrc)
		if err != nil {
			return fmt.Errorf("failed to marshal image_source: %w", err)
		}

		imageSourceJSON = sql.NullString{String: string(data), Valid: true}
	}

	// Marshal metadata as JSON
	metadataJSON := "{}"
	if metadata.Meta.Metadata != nil {
		data, err := json.Marshal(metadata.Meta.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		metadataJSON = string(data)
	}

	mapSetID := programID
	if metadata.Handles.MapOwnerID != nil {
		mapSetID = *metadata.Handles.MapOwnerID
	} else {
		exists, err := s.programRecordExists(ctx, programID)
		if err != nil {
			return err
		}

		// Save also updates existing records. Only the first save for a
		// self-owned program creates its map set; a reused kernel id with
		// a surviving map set still attempts the insert and fails closed.
		if !exists {
			if err := s.insertSelfOwnedMapSet(ctx, programID, metadata); err != nil {
				return err
			}
		}
	}
	// Handle nullable fields
	var attachFunc, owner, description, license, sourcePath sql.NullString
	if metadata.Load.AttachFunc() != "" {
		attachFunc = sql.NullString{String: metadata.Load.AttachFunc(), Valid: true}
	}
	if metadata.Load.SourcePath() != "" {
		sourcePath = sql.NullString{String: metadata.Load.SourcePath(), Valid: true}
	}
	if metadata.Meta.Owner != "" {
		owner = sql.NullString{String: metadata.Meta.Owner, Valid: true}
	}
	if metadata.Meta.Description != "" {
		description = sql.NullString{String: metadata.Meta.Description, Valid: true}
	}
	if metadata.License != "" {
		license = sql.NullString{String: metadata.License, Valid: true}
	}

	// Persist the in-memory record's UpdatedAt when present
	// (nil means "no update has happened yet" and the column
	// stays NULL so Get reads the same null back).
	var updatedAtNullable sql.NullString
	if metadata.UpdatedAt != nil {
		updatedAtNullable = sql.NullString{
			String: metadata.UpdatedAt.UTC().Format(time.RFC3339),
			Valid:  true,
		}
	}

	// Convert bool to int for SQLite
	var gplCompatibleInt int
	if metadata.GPLCompatible {
		gplCompatibleInt = 1
	}

	start := time.Now()
	result, err := s.stmtSaveProgram.ExecContext(ctx,
		programID,
		metadata.Meta.Name,
		metadata.Load.ProgramType().String(),
		metadata.Load.ObjectPath(),
		sourcePath,
		metadata.Handles.PinPath,
		attachFunc,
		globalDataJSON,
		mapSetID,
		imageSourceJSON,
		owner,
		description,
		license,
		gplCompatibleInt,
		metadataJSON,
		metadata.CreatedAt.UTC().Format(time.RFC3339),
		updatedAtNullable,
	)
	if err != nil {
		s.logger.Debug("sql", "stmt", "SaveProgram", "args", []any{programID, metadata.Meta.Name, "(columns)"}, "duration_ms", msec(time.Since(start)), "error", err)
		return fmt.Errorf("failed to insert program: %w", err)
	}

	rows, _ := result.RowsAffected()
	s.logger.Debug("sql", "stmt", "SaveProgram", "args", []any{programID, metadata.Meta.Name, "(columns)"}, "duration_ms", msec(time.Since(start)), "rows_affected", rows)

	return nil
}

// Delete removes program metadata. Returns ErrRecordNotFound if the
// program does not exist.
func (s *sqliteStore) Delete(ctx context.Context, programID kernel.ProgramID) error {
	start := time.Now()
	result, err := s.stmtDeleteProgram.ExecContext(ctx, programID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "DeleteProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	s.logger.Debug("sql", "stmt", "DeleteProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "rows_affected", rows)
	if rows == 0 {
		return fmt.Errorf("program %d: %w", programID, platform.ErrRecordNotFound)
	}
	return nil
}

func (s *sqliteStore) programRecordExists(ctx context.Context, programID kernel.ProgramID) (bool, error) {
	start := time.Now()
	var exists bool
	err := s.stmtProgramExists.QueryRowContext(ctx, programID).Scan(&exists)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ProgramRecordExists", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return false, err
	}

	s.logger.Debug("sql", "stmt", "ProgramRecordExists", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "exists", exists)
	return exists, nil
}

// insertSelfOwnedMapSet creates the map set for a newly loaded
// self-owned program. This is intentionally insert-only: if a kernel
// program id is reused while an old map set with that id is still alive,
// the primary-key collision is the fail-closed guard. Do not convert
// this to upsert, INSERT OR IGNORE, or open-or-create semantics.
func (s *sqliteStore) insertSelfOwnedMapSet(ctx context.Context, programID kernel.ProgramID, metadata bpfman.ProgramRecord) error {
	start := time.Now()
	_, err := s.stmtInsertMapSet.ExecContext(ctx,
		programID,
		metadata.Handles.MapsDir.String(),
		metadata.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		s.logger.Debug("sql", "stmt", "InsertMapSet", "args", []any{programID, metadata.Handles.MapsDir.String()}, "duration_ms", msec(time.Since(start)), "error", err)
		// A primary-key collision means a map set with this id already
		// exists, which under the insert-only contract can only be a
		// reused kernel program id meeting a still-alive map set. Name
		// that cause rather than surfacing a bare constraint violation.
		// A constraint failure does not abort the transaction, so the
		// follow-up read is safe on the same connection.
		if exists, existsErr := s.MapSetExists(ctx, programID); existsErr == nil && exists {
			return fmt.Errorf("create map set %d: %w", programID, platform.ErrMapSetIDReused)
		}
		return fmt.Errorf("create map set %d: %w", programID, err)
	}

	s.logger.Debug("sql", "stmt", "InsertMapSet", "args", []any{programID, metadata.Handles.MapsDir.String()}, "duration_ms", msec(time.Since(start)), "rows_affected", 1)
	return nil
}

// CountMapSets returns the number of map sets currently recorded.
func (s *sqliteStore) CountMapSets(ctx context.Context) (int, error) {
	start := time.Now()
	var count int
	err := s.stmtCountMapSets.QueryRowContext(ctx).Scan(&count)
	if err != nil {
		s.logger.Debug("sql", "stmt", "CountMapSets", "duration_ms", msec(time.Since(start)), "error", err)
		return 0, err
	}

	s.logger.Debug("sql", "stmt", "CountMapSets", "duration_ms", msec(time.Since(start)), "count", count)
	return count, nil
}

// CountMapSetUsers returns the number of programs that reference the
// map set identified by mapSetID, including its owner.
func (s *sqliteStore) CountMapSetUsers(ctx context.Context, mapSetID kernel.ProgramID) (int, error) {
	start := time.Now()
	var count int
	err := s.stmtCountMapSetUsers.QueryRowContext(ctx, mapSetID).Scan(&count)
	if err != nil {
		s.logger.Debug("sql", "stmt", "CountMapSetUsers", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "error", err)
		return 0, err
	}

	s.logger.Debug("sql", "stmt", "CountMapSetUsers", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "count", count)
	return count, nil
}

// ListMapSetUsers returns the kernel IDs of the programs that
// reference the map set identified by mapSetID, in ascending order.
func (s *sqliteStore) ListMapSetUsers(ctx context.Context, mapSetID kernel.ProgramID) ([]kernel.ProgramID, error) {
	start := time.Now()
	rows, err := s.stmtListMapSetUsers.QueryContext(ctx, mapSetID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListMapSetUsers", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	defer rows.Close()

	var users []kernel.ProgramID
	for rows.Next() {
		var id kernel.ProgramID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		users = append(users, id)
	}
	if err := rows.Err(); err != nil {
		s.logger.Debug("sql", "stmt", "ListMapSetUsers", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	s.logger.Debug("sql", "stmt", "ListMapSetUsers", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "count", len(users))
	return users, nil
}

// MapSetExists reports whether a map set with the given ID exists.
func (s *sqliteStore) MapSetExists(ctx context.Context, mapSetID kernel.ProgramID) (bool, error) {
	start := time.Now()
	var exists bool
	err := s.stmtMapSetExists.QueryRowContext(ctx, mapSetID).Scan(&exists)
	if err != nil {
		s.logger.Debug("sql", "stmt", "MapSetExists", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "error", err)
		return false, err
	}

	s.logger.Debug("sql", "stmt", "MapSetExists", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "exists", exists)
	return exists, nil
}

// DeleteMapSet removes the map set identified by mapSetID, returning
// platform.ErrRecordNotFound if no such map set exists.
func (s *sqliteStore) DeleteMapSet(ctx context.Context, mapSetID kernel.ProgramID) error {
	start := time.Now()
	result, err := s.stmtDeleteMapSet.ExecContext(ctx, mapSetID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "DeleteMapSet", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "error", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	s.logger.Debug("sql", "stmt", "DeleteMapSet", "args", []any{mapSetID}, "duration_ms", msec(time.Since(start)), "rows_affected", rows)
	if rows == 0 {
		return fmt.Errorf("map set %d: %w", mapSetID, platform.ErrRecordNotFound)
	}
	return nil
}

// List returns all program metadata. The returned map has no guaranteed
// iteration order; sorting for deterministic output is done in inspect.Snapshot.
func (s *sqliteStore) List(ctx context.Context) (map[kernel.ProgramID]bpfman.ProgramRecord, error) {
	start := time.Now()
	rows, err := s.stmtListPrograms.QueryContext(ctx)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListPrograms", "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	defer rows.Close()

	result := make(map[kernel.ProgramID]bpfman.ProgramRecord)
	for rows.Next() {
		programID, prog, err := s.scanProgramFromRows(rows)
		if err != nil {
			return nil, err
		}

		prog.ProgramID = programID
		result[programID] = prog
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.logger.Debug("sql", "stmt", "ListPrograms", "duration_ms", msec(time.Since(start)), "rows", len(result))
	return result, nil
}

// scanProgramFromRows scans a single row from *sql.Rows into a ProgramRecord struct.
// The row must include the program_id column followed by the standard columns.
func (s *sqliteStore) scanProgramFromRows(rows *sql.Rows) (kernel.ProgramID, bpfman.ProgramRecord, error) {
	var programID kernel.ProgramID
	var sp scannedProgram
	err := rows.Scan(
		&programID,
		&sp.programName,
		&sp.programTypeStr,
		&sp.objectPath,
		&sp.sourcePath,
		&sp.pinPath,
		&sp.attachFunc,
		&sp.globalDataJSON,
		&sp.mapSetID,
		&sp.mapPinPath,
		&sp.imageSourceJSON,
		&sp.owner,
		&sp.description,
		&sp.license,
		&sp.gplCompatible,
		&sp.createdAtStr,
		&sp.updatedAtStr,
		&sp.metadataJSON,
	)
	if err != nil {
		return 0, bpfman.ProgramRecord{}, err
	}

	sp.programID = programID
	prog, err := buildProgramRecord(&sp)
	if err != nil {
		return 0, bpfman.ProgramRecord{}, err
	}
	return programID, prog, nil
}
