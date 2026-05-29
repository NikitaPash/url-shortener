package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NikitaPash/url-shortener/internal/domain"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailTaken   = errors.New("email already taken")
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, email, passwordHash string) (*domain.User, error) {
	user := &domain.User{}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ($1, $2)
		 RETURNING id, email, is_admin, created_at`,
		email, passwordHash,
	).Scan(&user.ID, &user.Email, &user.IsAdmin, &user.CreatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return user, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	user := &domain.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, is_admin, created_at
		 FROM users WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

// EnsureAdmin upserts the seeded admin account: it creates the user if the
// email is new, or promotes the existing account and resets its password to the
// configured value. The env-provided credentials are therefore authoritative.
func (r *UserRepo) EnsureAdmin(ctx context.Context, email, passwordHash string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (email, password_hash, is_admin)
		 VALUES ($1, $2, true)
		 ON CONFLICT (email) DO UPDATE
		   SET is_admin = true,
		       password_hash = EXCLUDED.password_hash`,
		email, passwordHash,
	)
	return err
}
