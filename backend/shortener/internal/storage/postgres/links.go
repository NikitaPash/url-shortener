package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/NikitaPash/url-shortener/internal/domain"
)

var tracer = otel.Tracer("storage/postgres")

var (
	ErrLinkNotFound = errors.New("link not found")
	ErrIDTaken      = errors.New("short id already taken")
)

type LinkRepo struct {
	pool *pgxpool.Pool
}

func NewLinkRepo(pool *pgxpool.Pool) *LinkRepo {
	return &LinkRepo{pool: pool}
}

func (r *LinkRepo) Create(ctx context.Context, link *domain.Link) error {
	ctx, span := tracer.Start(ctx, "postgres.CreateLink")
	defer span.End()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO links (id, user_id, original_url, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		link.ID, link.UserID, link.OriginalURL, link.ExpiresAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrIDTaken
		}
		return err
	}
	return nil
}

func (r *LinkRepo) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Link, int64, error) {
	ctx, span := tracer.Start(ctx, "postgres.GetLinksByUserID")
	defer span.End()

	const q = `
		SELECT id, user_id, original_url, created_at, expires_at, is_active,
		       COUNT(*) OVER() AS total
		FROM links
		WHERE user_id = $1
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var links []domain.Link
	var total int64
	for rows.Next() {
		var link domain.Link
		if err := rows.Scan(
			&link.ID, &link.UserID, &link.OriginalURL, &link.CreatedAt, &link.ExpiresAt,
			&link.IsActive, &total,
		); err != nil {
			return nil, 0, err
		}
		links = append(links, link)
	}
	return links, total, rows.Err()
}

// GetByID returns a link by ID regardless of is_active status.
// Callers that need redirect-safe resolution must check IsActive themselves.
func (r *LinkRepo) GetByID(ctx context.Context, id string) (*domain.Link, error) {
	ctx, span := tracer.Start(ctx, "postgres.GetLinkByID")
	defer span.End()

	link := &domain.Link{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, original_url, created_at, expires_at, is_active
		 FROM links
		 WHERE id = $1
		   AND (expires_at IS NULL OR expires_at > now())`,
		id,
	).Scan(&link.ID, &link.UserID, &link.OriginalURL, &link.CreatedAt, &link.ExpiresAt, &link.IsActive)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	return link, err
}

func (r *LinkRepo) SetActive(ctx context.Context, id, userID string, active bool) error {
	ctx, span := tracer.Start(ctx, "postgres.SetLinkActive")
	defer span.End()

	_, err := r.pool.Exec(ctx,
		`UPDATE links SET is_active = $1 WHERE id = $2 AND user_id = $3`,
		active, id, userID,
	)
	return err
}

func (r *LinkRepo) Delete(ctx context.Context, id, userID string) error {
	ctx, span := tracer.Start(ctx, "postgres.DeleteLink")
	defer span.End()

	_, err := r.pool.Exec(ctx,
		`DELETE FROM links WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	return err
}
