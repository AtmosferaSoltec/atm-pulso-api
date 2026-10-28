package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

type queryReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type clientTransaction struct{ transaction pgx.Tx }

func translateWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "users_document_number_key" {
			return domain.ErrDocumentTaken
		}
		if pgErr.Code == "23505" {
			return domain.ErrConflict
		}
	}
	return err
}

func (r *Repository) WithClient(ctx context.Context, id uuid.UUID, fn func(app.ClientTransaction) error) error {
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(transaction pgx.Tx) error {
		// Serializes both the first create and additions; a new client has no row to lock yet.
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id.String()); err != nil {
			return err
		}
		return fn(&clientTransaction{transaction: transaction})
	})
	return translateWrite(err)
}

func readClient(ctx context.Context, q queryReader, id uuid.UUID) (app.Client, error) {
	catalog, err := readCatalog(ctx, q, id)
	if err != nil {
		return app.Client{}, err
	}
	client := app.Client{ID: id, Name: catalog.Organization.Name, Gyms: catalog.Gyms}
	err = q.QueryRow(ctx, `SELECT id,document_number,full_name,is_active FROM users WHERE organization_id=$1 AND role='owner' ORDER BY created_at,id LIMIT 1`, id).Scan(&client.Owner.ID, &client.Owner.DocumentNumber, &client.Owner.FullName, &client.Owner.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Client{}, domain.ErrResourceNotFound
	}
	return client, err
}

func (t *clientTransaction) FindClient(ctx context.Context, id uuid.UUID) (app.Client, error) {
	return readClient(ctx, t.transaction, id)
}

func (t *clientTransaction) InsertClient(ctx context.Context, input app.NewClient, hash string) error {
	name := domain.NormalizeFullName(input.Owner.FirstNames + " " + input.Owner.LastNames)
	_, err := t.transaction.Exec(ctx, `INSERT INTO organizations(id,name,contact_document_number,contact_name,contact_email,contact_phone) VALUES($1,$2,$3,$2,$4,$5)`, input.ID, name, input.Owner.DocumentNumber, input.Owner.Email, input.Owner.Phone)
	if err != nil {
		return err
	}
	_, err = t.transaction.Exec(ctx, `INSERT INTO users(id,organization_id,document_number,full_name,phone,role,password_hash,must_change_password) VALUES($1,$2,$3,$4,$5,'owner',$6,true)`, ids.New(), input.ID, input.Owner.DocumentNumber, name, input.Owner.Phone, hash)
	if err != nil {
		return err
	}
	for _, gym := range input.Gyms {
		if gym.ID == uuid.Nil {
			gym.ID = ids.New()
		}
		if err := t.AddGym(ctx, input.ID, gym); err != nil {
			return err
		}
	}
	return nil
}

func (t *clientTransaction) AddGym(ctx context.Context, org uuid.UUID, input app.NewGym) error {
	_, err := t.transaction.Exec(ctx, `INSERT INTO gyms(id,organization_id,name,tax_id)VALUES($1,$2,$3,NULLIF($4,''))`, input.ID, org, strings.TrimSpace(input.Name), input.TaxID)
	if err != nil {
		return err
	}
	for _, branch := range input.Branches {
		if branch.ID == uuid.Nil {
			branch.ID = ids.New()
		}
		if err := t.AddBranch(ctx, org, input.ID, branch); err != nil {
			return err
		}
	}
	return nil
}

func (t *clientTransaction) AddBranch(ctx context.Context, org, gym uuid.UUID, input app.NewBranch) error {
	_, err := t.transaction.Exec(ctx, `INSERT INTO branches(id,organization_id,gym_id,name,address)VALUES($1,$2,$3,$4,NULLIF($5,''))`, input.ID, org, gym, strings.TrimSpace(input.Name), strings.TrimSpace(input.Address))
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) GymOrganization(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var org uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT organization_id FROM gyms WHERE id=$1`, id).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrResourceNotFound
	}
	return org, err
}

func (r *Repository) ListClients(ctx context.Context, search string, page, size int) ([]app.Client, int64, error) {
	const filter = `($1='' OR strpos(lower(o.name),lower($1))>0 OR EXISTS(SELECT 1 FROM gyms g WHERE g.organization_id=o.id AND (strpos(lower(g.name),lower($1))>0 OR strpos(COALESCE(g.tax_id,''),$1)>0)))`
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM organizations o WHERE `+filter, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT o.id FROM organizations o WHERE `+filter+` ORDER BY o.created_at DESC,o.id LIMIT $2 OFFSET $3`, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	var identifiers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		identifiers = append(identifiers, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	clients := make([]app.Client, 0, len(identifiers))
	for _, id := range identifiers {
		client, err := readClient(ctx, r.pool, id)
		if err != nil {
			return nil, 0, err
		}
		clients = append(clients, client)
	}
	return clients, total, nil
}
func (r *Repository) ClientSummary(ctx context.Context) (app.ClientSummary, error) {
	var result app.ClientSummary
	err := r.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM organizations),(SELECT count(*) FROM gyms),(SELECT count(*) FROM branches),(SELECT count(*) FROM users WHERE organization_id IS NOT NULL)`).Scan(&result.Clients, &result.Gyms, &result.Branches, &result.ClientUsers)
	return result, err
}
