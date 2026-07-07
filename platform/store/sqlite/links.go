package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// ----------------------------------------------------------------------------
// Link Registry Operations
// ----------------------------------------------------------------------------

// DeleteLink removes link metadata by link ID.
// Due to CASCADE, this also removes the corresponding detail table entry.
// Dispatcher-backed links (xdp, tc) cannot be deleted through this
// method; they must be removed via DispatcherStore snapshot operations.
func (s *sqliteStore) DeleteLink(ctx context.Context, linkID bpfman.LinkID) error {
	start := time.Now()

	// Check if this is a dispatcher-backed link.
	var kind string
	err := s.stmtGetLinkRegistry.QueryRowContext(ctx, linkID).Scan(
		new(int64), &kind, new(int64), new(sql.NullInt64), new(sql.NullString), new(sql.NullString), new(string))
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("link %d: %w", linkID, platform.ErrRecordNotFound)
	}
	if err != nil {
		return fmt.Errorf("check link kind: %w", err)
	}
	if kind == "xdp" || kind == "tc" {
		return fmt.Errorf("link %d is dispatcher-backed (%s): must be removed via DispatcherStore", linkID, kind)
	}

	result, err := s.stmtDeleteLink.ExecContext(ctx, linkID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "DeleteLink", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "error", err)
		return fmt.Errorf("failed to delete link: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	s.logger.Debug("sql", "stmt", "DeleteLink", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "rows_affected", rows)
	if rows == 0 {
		return fmt.Errorf("link %d: %w", linkID, platform.ErrRecordNotFound)
	}

	return nil
}

// CreatePendingLink persists a standalone link record before the
// kernel attach happens. The insert allocates the bpfman link ID and
// a second statement records the pin path {linksDir}/{link_id}; both
// run inside one transaction owned here, so no committed state has a
// link row without its pin path regardless of the caller.
// KernelLinkID stays NULL until FinaliseLink.
func (s *sqliteStore) CreatePendingLink(ctx context.Context, spec bpfman.LinkSpec, linksDir string) (bpfman.LinkRecord, error) {
	var record bpfman.LinkRecord
	err := s.runInTx(ctx, "create_pending_link", func(tx *sqliteStore) error {
		created, err := tx.createLink(ctx, spec)
		if err != nil {
			return err
		}

		record, err = tx.setLinkPinPath(ctx, created.ID, linksDir)
		return err
	})
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	record.Details = spec.Details
	return record, nil
}

// setLinkPinPath records pin_path = {linksDir}/{link_id} on the link
// row and returns the updated record without details. Callers own the
// transaction boundary.
func (s *sqliteStore) setLinkPinPath(ctx context.Context, id bpfman.LinkID, linksDir string) (bpfman.LinkRecord, error) {
	start := time.Now()
	pin := filepath.Join(linksDir, strconv.FormatUint(uint64(id), 10))
	row := s.stmtSetLinkPinPath.QueryRowContext(ctx, pin, id)
	record, err := s.scanLinkRecord(row)
	if err != nil {
		s.logger.Debug("sql", "stmt", "SetLinkPinPath", "args", []any{pin, id}, "duration_ms", msec(time.Since(start)), "error", err)
		return bpfman.LinkRecord{}, fmt.Errorf("failed to set link pin path: %w", err)
	}

	s.logger.Debug("sql", "stmt", "SetLinkPinPath", "args", []any{pin, id}, "duration_ms", msec(time.Since(start)))
	return record, nil
}

// FinaliseLink records the captured kernel link ID on a pending link
// row created by CreatePendingLink. Returns the updated record
// without details.
func (s *sqliteStore) FinaliseLink(ctx context.Context, linkID bpfman.LinkID, kernelLinkID *kernel.LinkID) (bpfman.LinkRecord, error) {
	start := time.Now()

	var kid sql.NullInt64
	if kernelLinkID != nil {
		kid = sql.NullInt64{Int64: int64(*kernelLinkID), Valid: true}
	}

	row := s.stmtFinaliseLink.QueryRowContext(ctx, kid, linkID)
	record, err := s.scanLinkRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return bpfman.LinkRecord{}, fmt.Errorf("link %d: %w", linkID, platform.ErrRecordNotFound)
	}
	if err != nil {
		s.logger.Debug("sql", "stmt", "FinaliseLink", "args", []any{kid, linkID}, "duration_ms", msec(time.Since(start)), "error", err)
		return bpfman.LinkRecord{}, fmt.Errorf("failed to finalise link: %w", err)
	}

	s.logger.Debug("sql", "stmt", "FinaliseLink", "args", []any{kid, linkID}, "duration_ms", msec(time.Since(start)))
	return record, nil
}

// GetLink retrieves link metadata by link ID using two-phase lookup.
func (s *sqliteStore) GetLink(ctx context.Context, linkID bpfman.LinkID) (bpfman.LinkRecord, error) {
	// Phase 1: Get summary from registry
	start := time.Now()
	row := s.stmtGetLinkRegistry.QueryRowContext(ctx, linkID)

	record, err := s.scanLinkRecord(row)
	if err != nil {
		s.logger.Debug("sql", "stmt", "GetLinkRegistry", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "rows", 0)
		return bpfman.LinkRecord{}, err
	}

	s.logger.Debug("sql", "stmt", "GetLinkRegistry", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "rows", 1)

	// Phase 2: Get details based on link kind
	details, err := s.getLinkDetails(ctx, record.Kind, record.ID)
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	record.Details = details

	return record, nil
}

// ListLinks returns all links with their details populated. The returned slice
// has no guaranteed order; sorting for deterministic output is done in
// inspect.Snapshot.
func (s *sqliteStore) ListLinks(ctx context.Context) ([]bpfman.LinkRecord, error) {
	start := time.Now()
	rows, err := s.stmtListLinks.QueryContext(ctx)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListLinks", "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	defer rows.Close()

	result, err := s.scanLinkRecords(rows)
	if err != nil {
		return nil, err
	}

	s.logger.Debug("sql", "stmt", "ListLinks", "duration_ms", msec(time.Since(start)), "rows", len(result))

	// Batch-fetch all details and populate links
	if err := s.populateLinkDetails(ctx, result); err != nil {
		return nil, err
	}

	return result, nil
}

// ListLinksByProgram returns all links for a given program ID with
// their details populated.
func (s *sqliteStore) ListLinksByProgram(ctx context.Context, programID kernel.ProgramID) ([]bpfman.LinkRecord, error) {
	start := time.Now()
	rows, err := s.stmtListLinksByProgram.QueryContext(ctx, programID)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListLinksByProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	defer rows.Close()

	result, err := s.scanLinkRecords(rows)
	if err != nil {
		return nil, err
	}

	s.logger.Debug("sql", "stmt", "ListLinksByProgram", "args", []any{programID}, "duration_ms", msec(time.Since(start)), "rows", len(result))

	// Batch-fetch all details and populate links
	if err := s.populateLinkDetails(ctx, result); err != nil {
		return nil, err
	}

	return result, nil
}

// ListTCXLinksByInterface returns all TCX links for a given interface/direction/namespace.
// Used for computing attach order based on priority.
func (s *sqliteStore) ListTCXLinksByInterface(ctx context.Context, nsid uint64, ifindex uint32, direction string) ([]bpfman.TCXLinkInfo, error) {
	start := time.Now()
	rows, err := s.stmtListTCXLinksByInterface.QueryContext(ctx, nsid, ifindex, direction)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListTCXLinksByInterface", "args", []any{nsid, ifindex, direction}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	defer rows.Close()

	result := make([]bpfman.TCXLinkInfo, 0)
	for rows.Next() {
		var info bpfman.TCXLinkInfo
		var kernelLinkID int64
		if err := rows.Scan(&info.LinkID, &kernelLinkID, &info.KernelProgramID, &info.Priority); err != nil {
			return nil, fmt.Errorf("scan TCX link info: %w", err)
		}

		info.KernelLinkID = kernel.LinkID(kernelLinkID)
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate TCX links: %w", err)
	}

	s.logger.Debug("sql", "stmt", "ListTCXLinksByInterface", "args", []any{nsid, ifindex, direction}, "duration_ms", msec(time.Since(start)), "rows", len(result))
	return result, nil
}

// ----------------------------------------------------------------------------
// Batch Detail Population
// ----------------------------------------------------------------------------

// batchPopulateDetails queries all rows from a detail table and
// populates the Details field of matching links. The scanRow closure
// handles type-specific column scanning and post-processing.
func (s *sqliteStore) batchPopulateDetails(
	ctx context.Context,
	stmt *sql.Stmt,
	label string,
	links []bpfman.LinkRecord,
	linkIndex map[bpfman.LinkID]int,
	scanRow func(*sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error),
) error {
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return fmt.Errorf("batch fetch %s details: %w", label, err)
	}

	defer rows.Close()

	for rows.Next() {
		linkID, details, err := scanRow(rows)
		if err != nil {
			return fmt.Errorf("scan %s details: %w", label, err)
		}

		if idx, ok := linkIndex[linkID]; ok {
			links[idx].Details = details
		}
	}
	return rows.Err()
}

// populateLinkDetails batch-fetches details from all detail tables and
// populates the Details field of each link. This is O(8) queries regardless
// of N links, rather than O(N+1) for per-link fetching.
func (s *sqliteStore) populateLinkDetails(ctx context.Context, links []bpfman.LinkRecord) error {
	if len(links) == 0 {
		return nil
	}

	linkIndex := make(map[bpfman.LinkID]int, len(links))
	for i := range links {
		linkIndex[links[i].ID] = i
	}

	type batchEntry struct {
		stmt    *sql.Stmt
		label   string
		scanRow func(*sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error)
	}

	batches := []batchEntry{
		{s.stmtListAllTracepointDetails, "tracepoint", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.TracepointDetails
			if err := rows.Scan(&linkID, &d.Group, &d.Name); err != nil {
				return 0, nil, err
			}
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllKprobeDetails, "kprobe", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.KprobeDetails
			var retprobe int
			if err := rows.Scan(&linkID, &d.FnName, &d.Offset, &retprobe); err != nil {
				return 0, nil, err
			}
			d.Retprobe = retprobe == 1
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllUprobeDetails, "uprobe", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.UprobeDetails
			var fnName sql.NullString
			var pid sql.NullInt64
			var containerPid sql.NullInt64
			var retprobe int
			if err := rows.Scan(&linkID, &d.Target, &fnName, &d.Offset, &pid, &containerPid, &retprobe); err != nil {
				return 0, nil, err
			}
			if fnName.Valid {
				d.FnName = fnName.String
			}
			if pid.Valid {
				d.PID = int32(pid.Int64)
			}
			if containerPid.Valid {
				d.ContainerPid = int32(containerPid.Int64)
			}
			d.Retprobe = retprobe == 1
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllFentryDetails, "fentry", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.FentryDetails
			if err := rows.Scan(&linkID, &d.FnName); err != nil {
				return 0, nil, err
			}
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllFexitDetails, "fexit", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.FexitDetails
			if err := rows.Scan(&linkID, &d.FnName); err != nil {
				return 0, nil, err
			}
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllXDPDetails, "xdp", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.XDPDetails
			var proceedOnJSON string
			var netns sql.NullString
			if err := rows.Scan(&linkID, &d.Interface, &d.Ifindex, &d.Priority, &d.Position,
				&proceedOnJSON, &netns, &d.Nsid, &d.DispatcherID, &d.Revision); err != nil {
				return 0, nil, err
			}
			if err := json.Unmarshal([]byte(proceedOnJSON), &d.ProceedOn); err != nil {
				return 0, nil, fmt.Errorf("unmarshal xdp proceed_on: %w", err)
			}
			if netns.Valid {
				d.Netns = netns.String
			}
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllTCDetails, "tc", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.TCDetails
			var dirStr string
			var proceedOnJSON string
			var netns sql.NullString
			if err := rows.Scan(&linkID, &d.Interface, &d.Ifindex, &dirStr, &d.Priority, &d.Position,
				&proceedOnJSON, &netns, &d.Nsid, &d.DispatcherID, &d.Revision); err != nil {
				return 0, nil, err
			}
			dir, err := bpfman.ParseTCDirection(dirStr)
			if err != nil {
				return 0, nil, fmt.Errorf("invalid tc direction in DB for link %d: %w", linkID, err)
			}
			d.Direction = dir
			if err := json.Unmarshal([]byte(proceedOnJSON), &d.ProceedOn); err != nil {
				return 0, nil, fmt.Errorf("unmarshal tc proceed_on: %w", err)
			}
			if netns.Valid {
				d.Netns = netns.String
			}
			return bpfman.LinkID(linkID), d, nil
		}},
		{s.stmtListAllTCXDetails, "tcx", func(rows *sql.Rows) (bpfman.LinkID, bpfman.LinkDetails, error) {
			var linkID int64
			var d bpfman.TCXDetails
			var dirStr string
			var netns sql.NullString
			var nsid sql.NullInt64
			if err := rows.Scan(&linkID, &d.Interface, &d.Ifindex, &dirStr, &d.Priority, &netns, &nsid, &d.Position); err != nil {
				return 0, nil, err
			}
			dir, err := bpfman.ParseTCDirection(dirStr)
			if err != nil {
				return 0, nil, fmt.Errorf("invalid tcx direction in DB for link %d: %w", linkID, err)
			}
			d.Direction = dir
			if netns.Valid {
				d.Netns = netns.String
			}
			if nsid.Valid {
				d.Nsid = uint64(nsid.Int64)
			}
			return bpfman.LinkID(linkID), d, nil
		}},
	}

	for _, b := range batches {
		if err := s.batchPopulateDetails(ctx, b.stmt, b.label, links, linkIndex, b.scanRow); err != nil {
			return err
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// CreateLink - Unified Link Create Method
// ----------------------------------------------------------------------------

// CreateLink saves a link spec with its details and returns the store-allocated
// bpfman management handle.
// Dispatches to the appropriate detail table based on spec.Details.Kind().
// The registry row and detail row are inserted inside one transaction
// owned here, so every caller gets the schema's atomicity guarantee.
func (s *sqliteStore) CreateLink(ctx context.Context, spec bpfman.LinkSpec) (bpfman.LinkRecord, error) {
	var record bpfman.LinkRecord
	err := s.runInTx(ctx, "create_link", func(tx *sqliteStore) error {
		var err error
		record, err = tx.createLink(ctx, spec)
		return err
	})
	if err != nil {
		return bpfman.LinkRecord{}, err
	}
	return record, nil
}

// createLink inserts the registry row and the kind-specific detail
// row. Callers own the transaction boundary.
func (s *sqliteStore) createLink(ctx context.Context, spec bpfman.LinkSpec) (bpfman.LinkRecord, error) {
	switch spec.Kind {
	case bpfman.LinkKindXDP, bpfman.LinkKindTC:
		return bpfman.LinkRecord{}, fmt.Errorf("%s links are dispatcher-backed; use ReplaceDispatcherSnapshot", spec.Kind)
	}

	record, err := s.insertLinkRegistry(ctx, spec)
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	if spec.Details == nil {
		return record, nil
	}

	id := record.ID
	switch d := spec.Details.(type) {
	case bpfman.TracepointDetails:
		err = s.saveDetails(ctx, s.stmtSaveTracepointDetails, "Tracepoint", func() ([]any, error) {
			return []any{id, d.Group, d.Name}, nil
		})
	case bpfman.KprobeDetails:
		err = s.saveDetails(ctx, s.stmtSaveKprobeDetails, "Kprobe", func() ([]any, error) {
			retprobe := 0
			if d.Retprobe {
				retprobe = 1
			}
			return []any{id, d.FnName, d.Offset, retprobe}, nil
		})
	case bpfman.UprobeDetails:
		err = s.saveDetails(ctx, s.stmtSaveUprobeDetails, "Uprobe", func() ([]any, error) {
			retprobe := 0
			if d.Retprobe {
				retprobe = 1
			}
			return []any{id, d.Target, d.FnName, d.Offset, d.PID, d.ContainerPid, retprobe}, nil
		})
	case bpfman.FentryDetails:
		err = s.saveDetails(ctx, s.stmtSaveFentryDetails, "Fentry", func() ([]any, error) {
			return []any{id, d.FnName}, nil
		})
	case bpfman.FexitDetails:
		err = s.saveDetails(ctx, s.stmtSaveFexitDetails, "Fexit", func() ([]any, error) {
			return []any{id, d.FnName}, nil
		})
	case bpfman.XDPDetails:
		err = s.saveDetails(ctx, s.stmtSaveXDPDetails, "XDP", func() ([]any, error) {
			proceedOnJSON, err := json.Marshal(d.ProceedOn)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal proceed_on: %w", err)
			}
			return []any{id, d.Interface, d.Ifindex, d.Priority, d.Position,
				string(proceedOnJSON), d.Netns, d.Nsid, d.DispatcherID}, nil
		})
	case bpfman.TCDetails:
		err = s.saveDetails(ctx, s.stmtSaveTCDetails, "TC", func() ([]any, error) {
			proceedOnJSON, err := json.Marshal(d.ProceedOn)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal proceed_on: %w", err)
			}
			return []any{id, d.Interface, d.Ifindex, d.Direction.String(), d.Priority, d.Position,
				string(proceedOnJSON), d.Netns, d.Nsid, d.DispatcherID}, nil
		})
	case bpfman.TCXDetails:
		err = s.saveDetails(ctx, s.stmtSaveTCXDetails, "TCX", func() ([]any, error) {
			return []any{id, d.Interface, d.Ifindex, d.Direction.String(), d.Priority, d.Netns, d.Nsid}, nil
		})
	default:
		return bpfman.LinkRecord{}, fmt.Errorf("unknown link details type: %T", d)
	}
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	record.Details = spec.Details
	return record, nil
}

// saveDetails executes an insert into a link detail table. The
// prepareArgs closure handles any type-specific marshalling and
// returns the argument list for ExecContext.
func (s *sqliteStore) saveDetails(
	ctx context.Context,
	stmt *sql.Stmt,
	label string,
	prepareArgs func() ([]any, error),
) error {
	args, err := prepareArgs()
	if err != nil {
		return err
	}

	start := time.Now()
	_, err = stmt.ExecContext(ctx, args...)
	if err != nil {
		s.logger.Debug("sql", "stmt", "Save"+label+"Details", "args", args, "duration_ms", msec(time.Since(start)), "error", err)
		return fmt.Errorf("failed to insert %s details: %w", label, err)
	}

	s.logger.Debug("sql", "stmt", "Save"+label+"Details", "args", args, "duration_ms", msec(time.Since(start)), "rows_affected", 1)
	return nil
}

// ----------------------------------------------------------------------------
// Helper Functions
// ----------------------------------------------------------------------------

// marshalLinkMetadata encodes user link metadata for the metadata_json
// column, mirroring the program metadata encoding. An empty or nil map
// encodes as "{}".
func marshalLinkMetadata(metadata map[string]string) (string, error) {
	if len(metadata) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal link metadata: %w", err)
	}
	return string(b), nil
}

// unmarshalLinkMetadata decodes the metadata_json column, returning nil
// when the value is absent or the empty object so a metadata-less link
// round-trips as nil rather than an empty map.
func unmarshalLinkMetadata(col sql.NullString, linkID int64) (map[string]string, error) {
	if !col.Valid || col.String == "" || col.String == "{}" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(col.String), &m); err != nil {
		return nil, fmt.Errorf("unmarshal link metadata for link %d: %w", linkID, err)
	}
	return m, nil
}

// insertLinkRegistry inserts a spec into the links table.
func (s *sqliteStore) insertLinkRegistry(ctx context.Context, spec bpfman.LinkSpec) (bpfman.LinkRecord, error) {
	start := time.Now()

	// Convert pin to nullable string for DB storage
	var pinPath sql.NullString
	if spec.PinPath != nil {
		pinPath = sql.NullString{String: spec.PinPath.String(), Valid: true}
	}

	var kernelLinkID sql.NullInt64
	if spec.KernelLinkID != nil {
		kernelLinkID = sql.NullInt64{Int64: int64(*spec.KernelLinkID), Valid: true}
	}

	metadataJSON, err := marshalLinkMetadata(spec.Metadata)
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	row := s.stmtInsertLinkRegistry.QueryRowContext(ctx,
		spec.Kind.String(), spec.ProgramID, kernelLinkID,
		pinPath, metadataJSON, time.Now().UTC().Format(time.RFC3339))
	record, err := s.scanLinkRecord(row)
	if err != nil {
		s.logger.Debug("sql", "stmt", "InsertLinkRegistry", "args", []any{spec.Kind, spec.ProgramID, kernelLinkID, pinPath, "(timestamp)"}, "duration_ms", msec(time.Since(start)), "error", err)
		return bpfman.LinkRecord{}, fmt.Errorf("failed to insert link: %w", err)
	}

	record.Details = spec.Details

	s.logger.Debug("sql", "stmt", "InsertLinkRegistry", "args", []any{spec.Kind, spec.ProgramID, kernelLinkID, pinPath, "(timestamp)"}, "duration_ms", msec(time.Since(start)))
	return record, nil
}

// scanLinkRecordFrom scans one link row (without details) using the
// given Scan function, which both *sql.Row and *sql.Rows provide, so the
// single-row and multi-row readers share this body.
// Row format: id, kind, kernel_prog_id, kernel_link_id, pin_path, metadata_json, created_at
func scanLinkRecordFrom(scan func(dest ...any) error) (bpfman.LinkRecord, error) {
	var linkID int64
	var kindStr string
	var programID kernel.ProgramID
	var kernelLinkID sql.NullInt64
	var pinPath sql.NullString
	var metadataJSON sql.NullString
	var createdAtStr string

	if err := scan(&linkID, &kindStr, &programID, &kernelLinkID, &pinPath, &metadataJSON, &createdAtStr); err != nil {
		return bpfman.LinkRecord{}, err
	}

	kind, err := bpfman.ParseLinkKind(kindStr)
	if err != nil {
		return bpfman.LinkRecord{}, fmt.Errorf("invalid link kind in DB for link %d: %w", linkID, err)
	}

	record := bpfman.LinkRecord{
		ID:        bpfman.LinkID(linkID),
		Kind:      kind,
		ProgramID: programID,
	}
	if kernelLinkID.Valid {
		id := kernel.LinkID(kernelLinkID.Int64)
		record.KernelLinkID = &id
	}
	if pinPath.Valid {
		pin := bpfman.LinkPath(pinPath.String)
		record.PinPath = &pin
	}
	record.Metadata, err = unmarshalLinkMetadata(metadataJSON, linkID)
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return bpfman.LinkRecord{}, fmt.Errorf("invalid created_at timestamp for link %d: %q: %w", linkID, createdAtStr, err)
	}

	record.CreatedAt = createdAt

	return record, nil
}

// scanLinkRecord scans a single row into a LinkRecord (without details),
// mapping a missing row to ErrRecordNotFound.
func (s *sqliteStore) scanLinkRecord(row *sql.Row) (bpfman.LinkRecord, error) {
	record, err := scanLinkRecordFrom(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return bpfman.LinkRecord{}, platform.ErrRecordNotFound
	}
	return record, err
}

// scanLinkRecords scans multiple rows into a slice of LinkRecord (without details).
func (s *sqliteStore) scanLinkRecords(rows *sql.Rows) ([]bpfman.LinkRecord, error) {
	var result []bpfman.LinkRecord

	for rows.Next() {
		record, err := scanLinkRecordFrom(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}

	return result, rows.Err()
}

// getDetailsFromRow queries a single row from a detail table and
// handles the three-branch error pattern (ErrNoRows / other / success)
// with logging. The scan closure handles type-specific column scanning
// and post-processing.
func (s *sqliteStore) getDetailsFromRow(
	ctx context.Context,
	stmt *sql.Stmt,
	label string,
	linkID bpfman.LinkID,
	scan func(*sql.Row) (bpfman.LinkDetails, error),
) (bpfman.LinkDetails, error) {
	start := time.Now()
	row := stmt.QueryRowContext(ctx, linkID)
	details, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		s.logger.Debug("sql", "stmt", "Get"+label+"Details", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "rows", 0)
		return nil, fmt.Errorf("%s details for %d: %w", label, linkID, platform.ErrRecordNotFound)
	}
	if err != nil {
		s.logger.Debug("sql", "stmt", "Get"+label+"Details", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "error", err)
		return nil, err
	}

	s.logger.Debug("sql", "stmt", "Get"+label+"Details", "args", []any{linkID}, "duration_ms", msec(time.Since(start)), "rows", 1)
	return details, nil
}

// getLinkDetails retrieves the type-specific details for a link.
func (s *sqliteStore) getLinkDetails(ctx context.Context, kind bpfman.LinkKind, linkID bpfman.LinkID) (bpfman.LinkDetails, error) {
	switch kind {
	case bpfman.LinkKindTracepoint:
		return s.getDetailsFromRow(ctx, s.stmtGetTracepointDetails, "tracepoint", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.TracepointDetails
			err := row.Scan(&d.Group, &d.Name)
			return d, err
		})
	case bpfman.LinkKindKprobe, bpfman.LinkKindKretprobe:
		return s.getDetailsFromRow(ctx, s.stmtGetKprobeDetails, "kprobe", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.KprobeDetails
			var retprobe int
			if err := row.Scan(&d.FnName, &d.Offset, &retprobe); err != nil {
				return d, err
			}
			d.Retprobe = retprobe == 1
			return d, nil
		})
	case bpfman.LinkKindUprobe, bpfman.LinkKindUretprobe:
		return s.getDetailsFromRow(ctx, s.stmtGetUprobeDetails, "uprobe", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.UprobeDetails
			var fnName sql.NullString
			var pid sql.NullInt64
			var containerPid sql.NullInt64
			var retprobe int
			if err := row.Scan(&d.Target, &fnName, &d.Offset, &pid, &containerPid, &retprobe); err != nil {
				return d, err
			}
			if fnName.Valid {
				d.FnName = fnName.String
			}
			if pid.Valid {
				d.PID = int32(pid.Int64)
			}
			if containerPid.Valid {
				d.ContainerPid = int32(containerPid.Int64)
			}
			d.Retprobe = retprobe == 1
			return d, nil
		})
	case bpfman.LinkKindFentry:
		return s.getDetailsFromRow(ctx, s.stmtGetFentryDetails, "fentry", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.FentryDetails
			err := row.Scan(&d.FnName)
			return d, err
		})
	case bpfman.LinkKindFexit:
		return s.getDetailsFromRow(ctx, s.stmtGetFexitDetails, "fexit", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.FexitDetails
			err := row.Scan(&d.FnName)
			return d, err
		})
	case bpfman.LinkKindXDP:
		return s.getDetailsFromRow(ctx, s.stmtGetXDPDetails, "xdp", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.XDPDetails
			var proceedOnJSON string
			var netns sql.NullString
			if err := row.Scan(&d.Interface, &d.Ifindex, &d.Priority, &d.Position,
				&proceedOnJSON, &netns, &d.Nsid, &d.DispatcherID, &d.Revision); err != nil {
				return d, err
			}
			if err := json.Unmarshal([]byte(proceedOnJSON), &d.ProceedOn); err != nil {
				return d, fmt.Errorf("failed to unmarshal proceed_on: %w", err)
			}
			if netns.Valid {
				d.Netns = netns.String
			}
			return d, nil
		})
	case bpfman.LinkKindTC:
		return s.getDetailsFromRow(ctx, s.stmtGetTCDetails, "tc", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.TCDetails
			var dirStr string
			var proceedOnJSON string
			var netns sql.NullString
			if err := row.Scan(&d.Interface, &d.Ifindex, &dirStr, &d.Priority, &d.Position,
				&proceedOnJSON, &netns, &d.Nsid, &d.DispatcherID, &d.Revision); err != nil {
				return d, err
			}
			dir, err := bpfman.ParseTCDirection(dirStr)
			if err != nil {
				return d, fmt.Errorf("invalid tc direction in DB for link %d: %w", linkID, err)
			}
			d.Direction = dir
			if err := json.Unmarshal([]byte(proceedOnJSON), &d.ProceedOn); err != nil {
				return d, fmt.Errorf("failed to unmarshal proceed_on: %w", err)
			}
			if netns.Valid {
				d.Netns = netns.String
			}
			return d, nil
		})
	case bpfman.LinkKindTCX:
		return s.getDetailsFromRow(ctx, s.stmtGetTCXDetails, "tcx", linkID, func(row *sql.Row) (bpfman.LinkDetails, error) {
			var d bpfman.TCXDetails
			var dirStr string
			var netns sql.NullString
			var nsid sql.NullInt64
			if err := row.Scan(&d.Interface, &d.Ifindex, &dirStr, &d.Priority, &netns, &nsid, &d.Position); err != nil {
				return d, err
			}
			dir, err := bpfman.ParseTCDirection(dirStr)
			if err != nil {
				return d, fmt.Errorf("invalid tcx direction in DB for link %d: %w", linkID, err)
			}
			d.Direction = dir
			if netns.Valid {
				d.Netns = netns.String
			}
			if nsid.Valid {
				d.Nsid = uint64(nsid.Int64)
			}
			return d, nil
		})
	default:
		return nil, fmt.Errorf("unknown link kind: %s", kind)
	}
}
