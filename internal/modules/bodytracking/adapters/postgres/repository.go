// Package postgres is the persistence adapter of body_tracking. Every query is limited to the gym; the access-log row
// of a read or a write is inserted in the same transaction as the read or the write it records.
package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Repository stores measurements, routines and the access log.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// MemberHomeBranch returns the member's home branch (the member must belong to the gym).
func (r *Repository) MemberHomeBranch(ctx context.Context, gymID, memberID uuid.UUID) (uuid.UUID, error) {
	var branch uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT home_branch_id FROM members WHERE gym_id = $1 AND id = $2`, gymID, memberID).Scan(&branch)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrMemberNotFound
	}
	return branch, err
}

// audit appends one row to the access log. It never receives values, only ids.
func audit(ctx context.Context, t pgx.Tx, gymID, memberID uuid.UUID, actor app.Actor, action, resource string, resourceID *uuid.UUID) error {
	_, err := t.Exec(ctx, `INSERT INTO body_access_log (gym_id, member_id, user_id, action, resource, resource_id) VALUES ($1,$2,$3,$4,$5,$6)`,
		gymID, memberID, actor.UserID, action, resource, resourceID)
	return err
}

func number(raw *string) *json.Number {
	if raw == nil {
		return nil
	}
	n := json.Number(*raw)
	return &n
}

// ---- Measurements ----

const measurementColumns = `m.id, m.member_id, to_char(m.measured_on, 'YYYY-MM-DD'), m.weight_kg::text, m.height_cm::text, m.body_fat_pct::text,
	m.waist_cm::text, m.chest_cm::text, m.hip_cm::text, m.notes, m.created_by, cu.full_name, m.created_at, m.updated_by, uu.full_name, m.updated_at`

const measurementJoins = ` FROM body_measurements m JOIN users cu ON cu.id = m.created_by JOIN users uu ON uu.id = m.updated_by`

func scanMeasurement(row pgx.Row) (app.MeasurementView, error) {
	var v app.MeasurementView
	var w, h, f, wa, c, hi *string
	err := row.Scan(&v.ID, &v.MemberID, &v.MeasuredOn, &w, &h, &f, &wa, &c, &hi, &v.Notes, &v.CreatedBy, &v.CreatedByName, &v.CreatedAt, &v.UpdatedBy, &v.UpdatedByName, &v.UpdatedAt)
	v.WeightKg, v.HeightCm, v.BodyFatPct, v.WaistCm, v.ChestCm, v.HipCm = number(w), number(h), number(f), number(wa), number(c), number(hi)
	return v, err
}

// Measurements lists the live measurements, newest first, and logs the read.
func (r *Repository) Measurements(ctx context.Context, gymID, memberID uuid.UUID, actor app.Actor) ([]app.MeasurementView, error) {
	out := []app.MeasurementView{}
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		rows, err := t.Query(ctx, `SELECT `+measurementColumns+measurementJoins+` WHERE m.gym_id = $1 AND m.member_id = $2 AND m.deleted_at IS NULL
			ORDER BY m.measured_on DESC, m.created_at DESC, m.id LIMIT 1000`, gymID, memberID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanMeasurement(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		return audit(ctx, t, gymID, memberID, actor, app.ActionRead, app.ResourceMeasurements, nil)
	})
	return out, err
}

func oneMeasurement(ctx context.Context, t pgx.Tx, gymID, memberID, id uuid.UUID) (app.MeasurementView, error) {
	v, err := scanMeasurement(t.QueryRow(ctx, `SELECT `+measurementColumns+measurementJoins+`
		WHERE m.gym_id = $1 AND m.member_id = $2 AND m.id = $3 AND m.deleted_at IS NULL`, gymID, memberID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, domain.ErrMeasurementNotFound
	}
	return v, err
}

// CreateMeasurement inserts, or answers the stored record when the client id was already used for this member.
func (r *Repository) CreateMeasurement(ctx context.Context, gymID, memberID, id uuid.UUID, m app.Measurement, actor app.Actor) (app.MeasurementView, bool, error) {
	var view app.MeasurementView
	existed := false
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `INSERT INTO body_measurements (id, gym_id, member_id, measured_on, weight_kg, height_cm, body_fat_pct, waist_cm, chest_cm, hip_cm, notes, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,$7::numeric,$8::numeric,$9::numeric,$10::numeric,$11,$12,$12) ON CONFLICT (id) DO NOTHING`,
			id, gymID, memberID, m.MeasuredOn, m.WeightKg, m.HeightCm, m.BodyFatPct, m.WaistCm, m.ChestCm, m.HipCm, m.Notes, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			existed = true
			view, err = oneMeasurement(ctx, t, gymID, memberID, id)
			if errors.Is(err, domain.ErrMeasurementNotFound) {
				return domain.ErrConflict // the id belongs to another gym/member or to a deleted record
			}
			return err
		}
		if err := audit(ctx, t, gymID, memberID, actor, app.ActionCreate, app.ResourceMeasurements, &id); err != nil {
			return err
		}
		view, err = oneMeasurement(ctx, t, gymID, memberID, id)
		return err
	})
	return view, existed, err
}

// UpdateMeasurement replaces the values of a live measurement.
func (r *Repository) UpdateMeasurement(ctx context.Context, gymID, memberID, id uuid.UUID, m app.Measurement, actor app.Actor) (app.MeasurementView, error) {
	var view app.MeasurementView
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `UPDATE body_measurements SET measured_on=$4, weight_kg=$5::numeric, height_cm=$6::numeric, body_fat_pct=$7::numeric,
			waist_cm=$8::numeric, chest_cm=$9::numeric, hip_cm=$10::numeric, notes=$11, updated_by=$12, updated_at=now()
			WHERE gym_id=$1 AND member_id=$2 AND id=$3 AND deleted_at IS NULL`,
			gymID, memberID, id, m.MeasuredOn, m.WeightKg, m.HeightCm, m.BodyFatPct, m.WaistCm, m.ChestCm, m.HipCm, m.Notes, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrMeasurementNotFound
		}
		if err := audit(ctx, t, gymID, memberID, actor, app.ActionUpdate, app.ResourceMeasurements, &id); err != nil {
			return err
		}
		view, err = oneMeasurement(ctx, t, gymID, memberID, id)
		return err
	})
	return view, err
}

// DeleteMeasurement marks a live measurement as deleted.
func (r *Repository) DeleteMeasurement(ctx context.Context, gymID, memberID, id uuid.UUID, actor app.Actor) error {
	return tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `UPDATE body_measurements SET deleted_by=$4, deleted_at=now(), updated_by=$4, updated_at=now()
			WHERE gym_id=$1 AND member_id=$2 AND id=$3 AND deleted_at IS NULL`, gymID, memberID, id, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrMeasurementNotFound
		}
		return audit(ctx, t, gymID, memberID, actor, app.ActionDelete, app.ResourceMeasurements, &id)
	})
}

// ---- Routines ----

const routineColumns = `r.id, r.member_id, to_char(r.started_on, 'YYYY-MM-DD'), r.title, r.content, r.notes, r.is_current,
	r.created_by, cu.full_name, r.created_at, r.updated_by, uu.full_name, r.updated_at`

const routineJoins = ` FROM body_routines r JOIN users cu ON cu.id = r.created_by JOIN users uu ON uu.id = r.updated_by`

func scanRoutine(row pgx.Row) (app.RoutineView, error) {
	var v app.RoutineView
	err := row.Scan(&v.ID, &v.MemberID, &v.StartedOn, &v.Title, &v.Content, &v.Notes, &v.IsCurrent, &v.CreatedBy, &v.CreatedByName, &v.CreatedAt, &v.UpdatedBy, &v.UpdatedByName, &v.UpdatedAt)
	return v, err
}

// Routines lists the live routines: current ones first, newest start first. The read is logged.
func (r *Repository) Routines(ctx context.Context, gymID, memberID uuid.UUID, actor app.Actor) ([]app.RoutineView, error) {
	out := []app.RoutineView{}
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		rows, err := t.Query(ctx, `SELECT `+routineColumns+routineJoins+` WHERE r.gym_id = $1 AND r.member_id = $2 AND r.deleted_at IS NULL
			ORDER BY r.is_current DESC, r.started_on DESC, r.created_at DESC, r.id LIMIT 500`, gymID, memberID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanRoutine(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		return audit(ctx, t, gymID, memberID, actor, app.ActionRead, app.ResourceRoutines, nil)
	})
	return out, err
}

func oneRoutine(ctx context.Context, t pgx.Tx, gymID, memberID, id uuid.UUID) (app.RoutineView, error) {
	v, err := scanRoutine(t.QueryRow(ctx, `SELECT `+routineColumns+routineJoins+`
		WHERE r.gym_id = $1 AND r.member_id = $2 AND r.id = $3 AND r.deleted_at IS NULL`, gymID, memberID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, domain.ErrRoutineNotFound
	}
	return v, err
}

// CreateRoutine inserts, or answers the stored record when the client id was already used for this member.
func (r *Repository) CreateRoutine(ctx context.Context, gymID, memberID, id uuid.UUID, in app.Routine, actor app.Actor) (app.RoutineView, bool, error) {
	var view app.RoutineView
	existed := false
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `INSERT INTO body_routines (id, gym_id, member_id, started_on, title, content, notes, is_current, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT (id) DO NOTHING`,
			id, gymID, memberID, in.StartedOn, in.Title, in.Content, in.Notes, in.IsCurrent, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			existed = true
			view, err = oneRoutine(ctx, t, gymID, memberID, id)
			if errors.Is(err, domain.ErrRoutineNotFound) {
				return domain.ErrConflict
			}
			return err
		}
		if err := audit(ctx, t, gymID, memberID, actor, app.ActionCreate, app.ResourceRoutines, &id); err != nil {
			return err
		}
		view, err = oneRoutine(ctx, t, gymID, memberID, id)
		return err
	})
	return view, existed, err
}

// UpdateRoutine replaces a live routine.
func (r *Repository) UpdateRoutine(ctx context.Context, gymID, memberID, id uuid.UUID, in app.Routine, actor app.Actor) (app.RoutineView, error) {
	var view app.RoutineView
	err := tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `UPDATE body_routines SET started_on=$4, title=$5, content=$6, notes=$7, is_current=$8, updated_by=$9, updated_at=now()
			WHERE gym_id=$1 AND member_id=$2 AND id=$3 AND deleted_at IS NULL`,
			gymID, memberID, id, in.StartedOn, in.Title, in.Content, in.Notes, in.IsCurrent, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrRoutineNotFound
		}
		if err := audit(ctx, t, gymID, memberID, actor, app.ActionUpdate, app.ResourceRoutines, &id); err != nil {
			return err
		}
		view, err = oneRoutine(ctx, t, gymID, memberID, id)
		return err
	})
	return view, err
}

// DeleteRoutine marks a live routine as deleted.
func (r *Repository) DeleteRoutine(ctx context.Context, gymID, memberID, id uuid.UUID, actor app.Actor) error {
	return tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `UPDATE body_routines SET deleted_by=$4, deleted_at=now(), updated_by=$4, updated_at=now()
			WHERE gym_id=$1 AND member_id=$2 AND id=$3 AND deleted_at IS NULL`, gymID, memberID, id, actor.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrRoutineNotFound
		}
		return audit(ctx, t, gymID, memberID, actor, app.ActionDelete, app.ResourceRoutines, &id)
	})
}

// ---- Access log ----

// AccessLog returns the newest rows of the log of a member.
func (r *Repository) AccessLog(ctx context.Context, gymID, memberID uuid.UUID, limit int) ([]app.AccessEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT l.id, l.user_id, u.full_name, l.action, l.resource, l.resource_id, l.occurred_at
		FROM body_access_log l JOIN users u ON u.id = l.user_id
		WHERE l.gym_id = $1 AND l.member_id = $2 ORDER BY l.occurred_at DESC, l.id DESC LIMIT $3`, gymID, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.AccessEntry{}
	for rows.Next() {
		var e app.AccessEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Action, &e.Resource, &e.ResourceID, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
