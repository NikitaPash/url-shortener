package service

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

const jtiLength = 16

var ErrInvalidCredentials = errors.New("invalid credentials")

// UserRepository is the persistence behavior AuthService depends on.
type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	EnsureAdmin(ctx context.Context, email, passwordHash string) error
}

// TokenDenylist revokes JWTs until their natural expiry.
type TokenDenylist interface {
	DenyJWT(ctx context.Context, jti string, expiration time.Duration)
}

type AuthService struct {
	userRepo  UserRepository
	denylist  TokenDenylist
	jwtSecret []byte
	jwtExpiry time.Duration
}

func NewAuthService(repo UserRepository, denylist TokenDenylist, secret string, expiry time.Duration) *AuthService {
	return &AuthService{
		userRepo:  repo,
		denylist:  denylist,
		jwtSecret: []byte(secret),
		jwtExpiry: expiry,
	}
}

func (s *AuthService) Register(ctx context.Context, email, password string) (*domain.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return s.userRepo.Create(ctx, email, string(hash))
}

// SeedAdmin creates (or promotes) the configured admin account on startup.
func (s *AuthService) SeedAdmin(ctx context.Context, email, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.userRepo.EnsureAdmin(ctx, email, string(hash))
}

func (s *AuthService) Login(ctx context.Context, email, password string) (string, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, postgres.ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	jti, err := gonanoid.New(jtiLength)
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":      user.ID,
		"iat":      now.Unix(),
		"exp":      now.Add(s.jwtExpiry).Unix(),
		"jti":      jti,
		"is_admin": user.IsAdmin,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *AuthService) Logout(ctx context.Context, tokenString string) error {
	token, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return errors.New("invalid claims")
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		return errors.New("token has no jti claim")
	}

	exp, _ := claims["exp"].(float64)
	remaining := time.Until(time.Unix(int64(exp), 0))
	if remaining <= 0 {
		return nil
	}

	s.denylist.DenyJWT(ctx, jti, remaining)
	return nil
}
