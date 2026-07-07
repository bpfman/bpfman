package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// ----------------------------------------------------------------------------
// Dispatcher Store Operations
// ----------------------------------------------------------------------------

// prepareDispatcherStatements prepares SQL statements for dispatcher
// operations.
func (s *sqliteStore) prepareDispatcherStatements(ctx context.Context) error {
	var err error

	const getDispatcherSQL = `SELECT revision, program_id, kernel_link_id, priority, filter_handle, netns
		FROM dispatchers WHERE type = ? AND nsid = ? AND ifindex = ?`

	s.stmtGetDispatcher, err = s.db.PrepareContext(ctx, getDispatcherSQL)
	if err != nil {
		return fmt.Errorf("prepare GetDispatcher: %w", err)
	}

	const getXDPMembersSQL = `
		SELECT d.position, d.priority, p.program_name, d.proceed_on,
		       p.pin_path, l.id, l.kernel_link_id, l.kernel_prog_id, l.pin_path, d.interface, l.metadata_json
		FROM link_xdp_details d
		JOIN links l ON d.id = l.id
		JOIN managed_programs p ON l.kernel_prog_id = p.program_id
		WHERE d.nsid = ? AND d.ifindex = ?
		ORDER BY d.priority ASC, p.program_name ASC`

	s.stmtGetXDPMembers, err = s.db.PrepareContext(ctx, getXDPMembersSQL)
	if err != nil {
		return fmt.Errorf("prepare GetXDPMembers: %w", err)
	}

	const getTCMembersSQL = `
		SELECT d.position, d.priority, p.program_name, d.proceed_on,
		       p.pin_path, l.id, l.kernel_link_id, l.kernel_prog_id, l.pin_path, d.interface, l.metadata_json
		FROM link_tc_details d
		JOIN links l ON d.id = l.id
		JOIN managed_programs p ON l.kernel_prog_id = p.program_id
		WHERE d.nsid = ? AND d.ifindex = ? AND d.direction = ?
		ORDER BY d.priority ASC, p.program_name ASC`

	s.stmtGetTCMembers, err = s.db.PrepareContext(ctx, getTCMembersSQL)
	if err != nil {
		return fmt.Errorf("prepare GetTCMembers: %w", err)
	}

	const listDispatcherSummariesSQL = `
		SELECT d.type, d.nsid, d.ifindex, d.revision, d.program_id, d.kernel_link_id,
		       d.priority, d.filter_handle, d.netns,
		    (SELECT COUNT(*) FROM link_xdp_details x
		     WHERE x.nsid = d.nsid AND x.ifindex = d.ifindex
		       AND d.type = 'xdp') +
		    (SELECT COUNT(*) FROM link_tc_details t
		     WHERE t.nsid = d.nsid AND t.ifindex = d.ifindex
		       AND t.direction = CASE d.type
		           WHEN 'tc-ingress' THEN 'ingress'
		           WHEN 'tc-egress' THEN 'egress'
		           ELSE '' END) AS member_count
		FROM dispatchers d`

	s.stmtListDispatcherSummaries, err = s.db.PrepareContext(ctx, listDispatcherSummariesSQL)
	if err != nil {
		return fmt.Errorf("prepare ListDispatcherSummaries: %w", err)
	}

	const deleteXDPExtLinksSQL = `DELETE FROM links WHERE id IN
		(SELECT id FROM link_xdp_details WHERE nsid = ? AND ifindex = ?)`

	s.stmtDeleteXDPExtLinks, err = s.db.PrepareContext(ctx, deleteXDPExtLinksSQL)
	if err != nil {
		return fmt.Errorf("prepare DeleteXDPExtLinks: %w", err)
	}

	const deleteTCExtLinksSQL = `DELETE FROM links WHERE id IN
		(SELECT id FROM link_tc_details WHERE nsid = ? AND ifindex = ? AND direction = ?)`

	s.stmtDeleteTCExtLinks, err = s.db.PrepareContext(ctx, deleteTCExtLinksSQL)
	if err != nil {
		return fmt.Errorf("prepare DeleteTCExtLinks: %w", err)
	}

	const upsertDispatcherSQL = `INSERT INTO dispatchers (type, nsid, ifindex, revision, program_id, kernel_link_id, priority, filter_handle, netns, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, nsid, ifindex) DO UPDATE SET
		  revision = excluded.revision,
		  program_id = excluded.program_id,
		  kernel_link_id = excluded.kernel_link_id,
		  priority = excluded.priority,
		  filter_handle = excluded.filter_handle,
		  netns = excluded.netns,
		  updated_at = excluded.updated_at`

	s.stmtUpsertDispatcher, err = s.db.PrepareContext(ctx, upsertDispatcherSQL)
	if err != nil {
		return fmt.Errorf("prepare UpsertDispatcher: %w", err)
	}

	const insertExtLinkSQL = `INSERT INTO links (kind, kernel_prog_id, kernel_link_id, pin_path, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, kind, kernel_prog_id, kernel_link_id, pin_path, metadata_json, created_at`

	s.stmtInsertExtLink, err = s.db.PrepareContext(ctx, insertExtLinkSQL)
	if err != nil {
		return fmt.Errorf("prepare InsertExtLink: %w", err)
	}

	const insertExtLinkWithIDSQL = `INSERT INTO links (id, kind, kernel_prog_id, kernel_link_id, pin_path, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, kind, kernel_prog_id, kernel_link_id, pin_path, metadata_json, created_at`

	s.stmtInsertExtLinkWithID, err = s.db.PrepareContext(ctx, insertExtLinkWithIDSQL)
	if err != nil {
		return fmt.Errorf("prepare InsertExtLinkWithID: %w", err)
	}

	const insertXDPDetailSQL = `INSERT INTO link_xdp_details
		(id, interface, ifindex, priority, position, proceed_on, netns, nsid, dispatcher_program_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	s.stmtInsertXDPDetail, err = s.db.PrepareContext(ctx, insertXDPDetailSQL)
	if err != nil {
		return fmt.Errorf("prepare InsertXDPDetail: %w", err)
	}

	const insertTCDetailSQL = `INSERT INTO link_tc_details
		(id, interface, ifindex, direction, priority, position, proceed_on, netns, nsid, dispatcher_program_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	s.stmtInsertTCDetail, err = s.db.PrepareContext(ctx, insertTCDetailSQL)
	if err != nil {
		return fmt.Errorf("prepare InsertTCDetail: %w", err)
	}

	const deleteDispatcherSQL = `DELETE FROM dispatchers WHERE type = ? AND nsid = ? AND ifindex = ?`

	s.stmtDeleteDispatcher, err = s.db.PrepareContext(ctx, deleteDispatcherSQL)
	if err != nil {
		return fmt.Errorf("prepare DeleteDispatcher: %w", err)
	}

	return nil
}

// dispatcherDirection returns the TC direction string for a
// dispatcher type. Returns "" for XDP.
func dispatcherDirection(dt dispatcher.DispatcherType) string {
	switch dt {
	case dispatcher.DispatcherTypeTCIngress:
		return "ingress"
	case dispatcher.DispatcherTypeTCEgress:
		return "egress"
	default:
		return ""
	}
}

// scanDispatcherRuntime scans the dispatcher row fields (program_id,
// link_id, priority, filter_handle) into a DispatcherRuntime, handling
// the nullable link_id, priority, and filter_handle.
func scanDispatcherRuntime(programID kernel.ProgramID, nullLinkID sql.NullInt64, priority sql.NullInt64, handle sql.NullInt64, netns string) platform.DispatcherRuntime {
	rt := platform.DispatcherRuntime{ProgramID: programID, NetnsPath: netns}
	if nullLinkID.Valid {
		lid := kernel.LinkID(nullLinkID.Int64)
		rt.KernelLinkID = &lid
	}
	if priority.Valid {
		p := uint16(priority.Int64)
		rt.FilterPriority = &p
	}
	if handle.Valid {
		h := uint32(handle.Int64)
		rt.FilterHandle = &h
	}
	return rt
}

// GetDispatcherSnapshot retrieves a complete snapshot of a dispatcher
// and all its extension members.
func (s *sqliteStore) GetDispatcherSnapshot(ctx context.Context, key dispatcher.Key) (platform.DispatcherSnapshot, error) {
	start := time.Now()

	// Fetch dispatcher row.
	var revision uint32
	var programID kernel.ProgramID
	var nullLinkID sql.NullInt64
	var priority sql.NullInt64
	var filterHandle sql.NullInt64
	var netnsPath string

	err := s.stmtGetDispatcher.QueryRowContext(ctx,
		key.Type.String(), key.Nsid, key.Ifindex,
	).Scan(&revision, &programID, &nullLinkID, &priority, &filterHandle, &netnsPath)
	if err == sql.ErrNoRows {
		s.logger.Debug("sql", "stmt", "GetDispatcherSnapshot", "args", []any{key}, "duration_ms", msec(time.Since(start)), "rows", 0)
		return platform.DispatcherSnapshot{}, fmt.Errorf("dispatcher (%s, %d, %d): %w", key.Type, key.Nsid, key.Ifindex, platform.ErrRecordNotFound)
	}
	if err != nil {
		s.logger.Debug("sql", "stmt", "GetDispatcherSnapshot", "args", []any{key}, "duration_ms", msec(time.Since(start)), "error", err)
		return platform.DispatcherSnapshot{}, fmt.Errorf("get dispatcher snapshot: %w", err)
	}

	snap := platform.DispatcherSnapshot{
		Key:      key,
		Revision: revision,
		Runtime:  scanDispatcherRuntime(programID, nullLinkID, priority, filterHandle, netnsPath),
	}

	// Fetch members from the appropriate detail table.
	var rows *sql.Rows

	if key.Type == dispatcher.DispatcherTypeXDP {
		rows, err = s.stmtGetXDPMembers.QueryContext(ctx, key.Nsid, key.Ifindex)
	} else {
		dir := dispatcherDirection(key.Type)
		rows, err = s.stmtGetTCMembers.QueryContext(ctx, key.Nsid, key.Ifindex, dir)
	}
	if err != nil {
		return platform.DispatcherSnapshot{}, fmt.Errorf("get dispatcher snapshot members: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var m platform.DispatcherMember
		var proceedOnJSON string
		var kernelLinkID sql.NullInt64
		var linkPinPath sql.NullString
		var metadataJSON sql.NullString
		if err := rows.Scan(&m.Position, &m.Priority, &m.ProgramName, &proceedOnJSON,
			&m.ProgPinPath, &m.LinkID, &kernelLinkID, &m.ProgramID, &linkPinPath, &m.Ifname, &metadataJSON); err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("scan dispatcher member: %w", err)
		}
		if kernelLinkID.Valid {
			id := kernel.LinkID(kernelLinkID.Int64)
			m.KernelLinkID = &id
		}
		if linkPinPath.Valid {
			m.LinkPinPath = bpfman.LinkPath(linkPinPath.String)
		}
		meta, err := unmarshalLinkMetadata(metadataJSON, int64(m.LinkID))
		if err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("scan dispatcher member metadata: %w", err)
		}

		m.Metadata = meta

		var actions []int32
		if err := json.Unmarshal([]byte(proceedOnJSON), &actions); err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("unmarshal proceed_on: %w", err)
		}

		bitmask, err := dispatcher.ProceedOnMask(key.Type, actions...)
		if err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("decode proceed_on for dispatcher member: %w", err)
		}

		m.ProceedOn = bitmask

		snap.Members = append(snap.Members, m)
	}
	if err := rows.Err(); err != nil {
		return platform.DispatcherSnapshot{}, fmt.Errorf("iterate dispatcher members: %w", err)
	}

	s.logger.Debug("sql", "stmt", "GetDispatcherSnapshot", "args", []any{key}, "duration_ms", msec(time.Since(start)), "members", len(snap.Members))
	return snap, nil
}

// ListDispatcherSummaries returns lightweight summaries of all
// dispatchers with member counts. Uses a correlated subquery to
// count members without joining detail tables.
func (s *sqliteStore) ListDispatcherSummaries(ctx context.Context) ([]platform.DispatcherSummary, error) {
	start := time.Now()

	rows, err := s.stmtListDispatcherSummaries.QueryContext(ctx)
	if err != nil {
		s.logger.Debug("sql", "stmt", "ListDispatcherSummaries", "duration_ms", msec(time.Since(start)), "error", err)
		return nil, fmt.Errorf("list dispatcher summaries: %w", err)
	}

	defer rows.Close()

	var result []platform.DispatcherSummary
	for rows.Next() {
		var dispTypeStr string
		var summary platform.DispatcherSummary
		var programID kernel.ProgramID
		var nullLinkID sql.NullInt64
		var priority sql.NullInt64
		var filterHandle sql.NullInt64
		var netnsPath string
		if err := rows.Scan(&dispTypeStr, &summary.Key.Nsid, &summary.Key.Ifindex,
			&summary.Revision, &programID, &nullLinkID, &priority, &filterHandle, &netnsPath, &summary.MemberCount); err != nil {
			s.logger.Debug("sql", "stmt", "ListDispatcherSummaries", "duration_ms", msec(time.Since(start)), "error", err)
			return nil, fmt.Errorf("scan dispatcher summary: %w", err)
		}

		parsed, err := dispatcher.ParseDispatcherType(dispTypeStr)
		if err != nil {
			return nil, fmt.Errorf("invalid dispatcher type in DB: %w", err)
		}

		summary.Key.Type = parsed
		summary.Runtime = scanDispatcherRuntime(programID, nullLinkID, priority, filterHandle, netnsPath)
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		s.logger.Debug("sql", "stmt", "ListDispatcherSummaries", "duration_ms", msec(time.Since(start)), "error", err)
		return nil, fmt.Errorf("iterate dispatcher summaries: %w", err)
	}

	s.logger.Debug("sql", "stmt", "ListDispatcherSummaries", "duration_ms", msec(time.Since(start)), "rows", len(result))
	return result, nil
}

// ReplaceDispatcherSnapshot atomically replaces all persisted state
// for a dispatcher's attach point. Deletes old extension link records
// by attach point, upserts the dispatcher row, and inserts new
// member link records, all inside one transaction owned here.
func (s *sqliteStore) ReplaceDispatcherSnapshot(ctx context.Context, snap platform.DispatcherSnapshotSpec) (platform.DispatcherSnapshot, error) {
	var completed platform.DispatcherSnapshot
	err := s.runInTx(ctx, "dispatcher_replace_snapshot", func(tx *sqliteStore) error {
		var err error
		completed, err = tx.replaceDispatcherSnapshot(ctx, snap)
		return err
	})
	if err != nil {
		return platform.DispatcherSnapshot{}, err
	}
	return completed, nil
}

// replaceDispatcherSnapshot performs the snapshot replacement
// statements. Callers own the transaction boundary.
func (s *sqliteStore) replaceDispatcherSnapshot(ctx context.Context, snap platform.DispatcherSnapshotSpec) (platform.DispatcherSnapshot, error) {
	start := time.Now()
	now := time.Now().UTC().Format(time.RFC3339)
	completed := platform.DispatcherSnapshot{
		Key:      snap.Key,
		Revision: snap.Revision,
		Runtime:  snap.Runtime,
	}

	// Step 1: Delete old extension link base rows by attach point.
	// CASCADE from links -> detail tables removes the detail rows.
	if snap.Key.Type == dispatcher.DispatcherTypeXDP {
		if _, err := s.stmtDeleteXDPExtLinks.ExecContext(ctx,
			snap.Key.Nsid, snap.Key.Ifindex); err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("delete old XDP extension links: %w", err)
		}
	} else {
		dir := dispatcherDirection(snap.Key.Type)
		if _, err := s.stmtDeleteTCExtLinks.ExecContext(ctx,
			snap.Key.Nsid, snap.Key.Ifindex, dir); err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("delete old TC extension links: %w", err)
		}
	}

	// Step 2: Upsert dispatcher row.
	var linkID sql.NullInt64
	if snap.Runtime.KernelLinkID != nil {
		linkID = sql.NullInt64{Int64: int64(*snap.Runtime.KernelLinkID), Valid: true}
	}
	var priority sql.NullInt64
	if snap.Runtime.FilterPriority != nil {
		priority = sql.NullInt64{Int64: int64(*snap.Runtime.FilterPriority), Valid: true}
	}
	var filterHandle sql.NullInt64
	if snap.Runtime.FilterHandle != nil {
		filterHandle = sql.NullInt64{Int64: int64(*snap.Runtime.FilterHandle), Valid: true}
	}
	if _, err := s.stmtUpsertDispatcher.ExecContext(ctx,
		snap.Key.Type.String(), snap.Key.Nsid, snap.Key.Ifindex,
		snap.Revision, snap.Runtime.ProgramID, linkID,
		priority, filterHandle, snap.Runtime.NetnsPath, now, now); err != nil {
		return platform.DispatcherSnapshot{}, fmt.Errorf("upsert dispatcher: %w", err)
	}

	// Step 3: Insert base link row and detail row for each member.
	for _, spec := range snap.Members {
		// Insert base link row.
		kind := "xdp"
		if snap.Key.Type != dispatcher.DispatcherTypeXDP {
			kind = "tc"
		}
		var pinPath sql.NullString
		if spec.LinkPinPath != "" {
			pinPath = sql.NullString{String: spec.LinkPinPath.String(), Valid: true}
		}
		var kernelLinkID sql.NullInt64
		if spec.KernelLinkID != nil {
			kernelLinkID = sql.NullInt64{Int64: int64(*spec.KernelLinkID), Valid: true}
		}
		metadataJSON, err := marshalLinkMetadata(spec.Metadata)
		if err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("marshal extension link metadata: %w", err)
		}

		var record bpfman.LinkRecord
		if spec.ExistingLinkID != nil {
			record, err = s.scanLinkRecord(s.stmtInsertExtLinkWithID.QueryRowContext(ctx,
				*spec.ExistingLinkID, kind, spec.ProgramID, kernelLinkID, pinPath, metadataJSON, now))
		} else {
			record, err = s.scanLinkRecord(s.stmtInsertExtLink.QueryRowContext(ctx,
				kind, spec.ProgramID, kernelLinkID, pinPath, metadataJSON, now))
		}
		if err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("insert extension link: %w", err)
		}

		m := platform.DispatcherMember{
			ProgramID:    spec.ProgramID,
			ProgramName:  spec.ProgramName,
			ProgPinPath:  spec.ProgPinPath,
			LinkID:       record.ID,
			KernelLinkID: spec.KernelLinkID,
			LinkPinPath:  spec.LinkPinPath,
			Position:     spec.Position,
			Priority:     spec.Priority,
			ProceedOn:    spec.ProceedOn,
			Ifname:       spec.Ifname,
			Metadata:     spec.Metadata,
		}

		// Insert detail row.
		proceedOnJSON, err := proceedOnToJSON(snap.Key.Type, m.ProceedOn)
		if err != nil {
			return platform.DispatcherSnapshot{}, fmt.Errorf("marshal proceed_on for link %d: %w", m.LinkID, err)
		}

		if snap.Key.Type == dispatcher.DispatcherTypeXDP {
			if _, err := s.stmtInsertXDPDetail.ExecContext(ctx,
				m.LinkID, m.Ifname, snap.Key.Ifindex, m.Priority, m.Position,
				proceedOnJSON, snap.Runtime.NetnsPath, snap.Key.Nsid, snap.Runtime.ProgramID); err != nil {
				return platform.DispatcherSnapshot{}, fmt.Errorf("insert XDP detail for link %d: %w", m.LinkID, err)
			}
		} else {
			dir := dispatcherDirection(snap.Key.Type)
			if _, err := s.stmtInsertTCDetail.ExecContext(ctx,
				m.LinkID, m.Ifname, snap.Key.Ifindex, dir, m.Priority, m.Position,
				proceedOnJSON, snap.Runtime.NetnsPath, snap.Key.Nsid, snap.Runtime.ProgramID); err != nil {
				return platform.DispatcherSnapshot{}, fmt.Errorf("insert TC detail for link %d: %w", m.LinkID, err)
			}
		}
		completed.Members = append(completed.Members, m)
	}

	s.logger.Debug("sql", "stmt", "ReplaceDispatcherSnapshot", "args", []any{snap.Key, snap.Revision}, "duration_ms", msec(time.Since(start)), "members", len(snap.Members))
	return completed, nil
}

// DeleteDispatcherSnapshot removes a dispatcher and all its extension
// link records by attach point key, inside one transaction owned
// here.
func (s *sqliteStore) DeleteDispatcherSnapshot(ctx context.Context, key dispatcher.Key) error {
	return s.runInTx(ctx, "dispatcher_delete_snapshot", func(tx *sqliteStore) error {
		return tx.deleteDispatcherSnapshot(ctx, key)
	})
}

// deleteDispatcherSnapshot performs the snapshot deletion statements.
// Callers own the transaction boundary.
func (s *sqliteStore) deleteDispatcherSnapshot(ctx context.Context, key dispatcher.Key) error {
	start := time.Now()

	// Step 1: Delete extension link base rows by attach point.
	// CASCADE removes the detail rows.
	if key.Type == dispatcher.DispatcherTypeXDP {
		if _, err := s.stmtDeleteXDPExtLinks.ExecContext(ctx,
			key.Nsid, key.Ifindex); err != nil {
			return fmt.Errorf("delete XDP extension links: %w", err)
		}
	} else {
		dir := dispatcherDirection(key.Type)
		if _, err := s.stmtDeleteTCExtLinks.ExecContext(ctx,
			key.Nsid, key.Ifindex, dir); err != nil {
			return fmt.Errorf("delete TC extension links: %w", err)
		}
	}

	// Step 2: Delete dispatcher row.
	result, err := s.stmtDeleteDispatcher.ExecContext(ctx,
		key.Type.String(), key.Nsid, key.Ifindex)
	if err != nil {
		return fmt.Errorf("delete dispatcher: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		s.logger.Debug("sql", "stmt", "DeleteDispatcherSnapshot", "args", []any{key}, "duration_ms", msec(time.Since(start)), "rows", 0)
		return fmt.Errorf("dispatcher (%s, %d, %d): %w", key.Type, key.Nsid, key.Ifindex, platform.ErrRecordNotFound)
	}

	s.logger.Debug("sql", "stmt", "DeleteDispatcherSnapshot", "args", []any{key}, "duration_ms", msec(time.Since(start)), "rows_affected", affected)
	return nil
}

// proceedOnToJSON converts a dispatcher ABI proceed-on bitmask to a JSON
// array of action codes, matching the storage format used by the schema.
func proceedOnToJSON(dispType dispatcher.DispatcherType, bitmask uint32) (string, error) {
	actions, err := dispatcher.ProceedOnActions(dispType, bitmask)
	if err != nil {
		return "", err
	}
	if len(actions) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(actions)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
